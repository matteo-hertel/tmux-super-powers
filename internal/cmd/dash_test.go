package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

func TestAgentDashboardDoesNotPoll(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	if cmd := model.Init(); cmd != nil {
		t.Fatal("dashboard Init returned a command; snapshots must refresh only on demand")
	}
}

func TestDiscoverAgentsReadsStoredOutputAfterDelegatedPaneCloses(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	session := fmt.Sprintf("tsp-dash-output-test-%d", time.Now().UnixNano())
	if err := tmuxpkg.CreateTwoPaneSession(session, dir, "sleep 5", ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tmuxpkg.KillSession(session)
	})
	panes := tmuxpkg.Panes(session)
	if len(panes) != 2 {
		t.Fatalf("pane count = %d, want 2", len(panes))
	}
	parentPane := panes[0]
	for _, pane := range panes {
		if pane.Index == 0 {
			parentPane = pane
		}
	}
	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	root, err := registry.RegisterManaged(service.SpawnResult{
		Task: "root", Session: session, PaneIndex: parentPane.Index, PaneID: parentPane.ID, WorktreePath: dir,
	}, service.AgentProviderCodex, parentPane.Index, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SpawnDelegatedAgent(
		"child", "child", dir, session, parentPane, "main", "",
		"sh -c 'printf delegate-finished' placeholder", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	child, err := registry.RegisterDelegated(result, service.AgentProviderClaude, root.ID, result.PaneIndex, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	childPane := tmuxpkg.Pane{ID: result.PaneID, Index: result.PaneIndex}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && tmuxpkg.PaneExists(session, childPane) {
		time.Sleep(20 * time.Millisecond)
	}

	agents, err := discoverAgents(registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		if agent.run.ID == child.ID {
			if !strings.Contains(agent.output, "delegate-finished") {
				t.Fatalf("stored delegated output = %q", agent.output)
			}
			return
		}
	}
	t.Fatalf("delegated run %q was not discovered", child.ID)
}

func TestAgentDashboardKeepsSelectionVisibleAndInsideTerminal(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	model.width = 96
	model.height = 18
	model.cursor = 24
	for index := 0; index < 30; index++ {
		model.agents = append(model.agents, agentEntry{
			run: service.AgentRun{
				ID:          fmt.Sprintf("run-%02d", index),
				SessionName: fmt.Sprintf("session-%02d", index),
				Task:        fmt.Sprintf("agent task %02d", index),
				PaneIndex:   1,
				StartedAt:   time.Now(),
			},
			live:          true,
			sessionExists: true,
		})
	}

	view := model.View()
	if !strings.Contains(view, "agent task 24") {
		t.Fatal("selected agent was not rendered")
	}
	lines := strings.Split(view, "\n")
	if len(lines) > model.height {
		t.Fatalf("view rendered %d lines for terminal height %d", len(lines), model.height)
	}
	for index, line := range lines {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("line %d width = %d, want <= %d", index, width, model.width)
		}
	}
}

func TestAgentDashboardBoundsWideOutput(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{{
		run: service.AgentRun{
			ID:          "run-wide",
			SessionName: "wide-session",
			Task:        "wide output",
			PaneIndex:   1,
		},
		output:        strings.Repeat("x", 300),
		live:          true,
		sessionExists: true,
	}}, &config.Config{}, nil, "/repo")
	model.width = 72
	model.height = 18

	for index, line := range strings.Split(model.View(), "\n") {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("line %d width = %d, want <= %d", index, width, model.width)
		}
	}
}

func TestAgentDashboardCountsIdleTmuxSessions(t *testing.T) {
	entries := []agentEntry{
		{
			run:  service.AgentRun{ID: "run-agent", SessionName: "agent-session", PaneIndex: 1},
			live: true, sessionExists: true,
		},
		{
			run:  service.AgentRun{ID: "run-child", SessionName: "agent-session", PaneIndex: 2},
			live: true, sessionExists: true,
		},
		{
			run:           service.AgentRun{ID: "run-idle", SessionName: "idle-session", PaneIndex: 0},
			sessionExists: true, sessionOnly: true,
		},
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	model.width = 100
	model.height = 20

	if got := model.sessionCount(); got != 2 {
		t.Fatalf("sessionCount() = %d, want 2", got)
	}
	if entries[2].status() != "idle" || entries[2].provider() != "session" {
		t.Fatalf("idle entry = %s/%s, want idle/session", entries[2].status(), entries[2].provider())
	}
	view := model.View()
	if !strings.Contains(view, "2 sessions · 2 active agents") {
		t.Fatalf("dashboard header does not show session count: %q", view)
	}
}

func TestAgentDashboardCompactRosterShowsSevenSessions(t *testing.T) {
	var entries []agentEntry
	for index := 0; index < 7; index++ {
		entries = append(entries, agentEntry{
			run: service.AgentRun{
				ID:          fmt.Sprintf("run-%d", index),
				SessionName: fmt.Sprintf("session-%d", index),
			},
			sessionExists: true,
			sessionOnly:   true,
		})
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	model.width = 80
	model.height = 24
	view := model.View()
	if !strings.Contains(view, "session-0") || !strings.Contains(view, "session-6") {
		t.Fatalf("compact roster did not show all seven sessions: %q", view)
	}
}

func TestDiscoverAgentsIncludesIdleTmuxSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	sessionName := fmt.Sprintf("tsp-idle-discovery-test-%d", time.Now().UnixNano())
	create := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", t.TempDir())
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v: %s", err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	entries, err := discoverAgents(registry)
	if err != nil {
		t.Fatalf("discoverAgents: %v", err)
	}
	for _, entry := range entries {
		if entry.run.SessionName == sessionName {
			if !entry.sessionOnly || entry.status() != "idle" {
				t.Fatalf("idle session entry = %#v", entry)
			}
			return
		}
	}
	t.Fatalf("idle tmux session %q was omitted", sessionName)
}

func TestAgentDashboardOpensSpawnFormWithCurrentProject(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		t.Fatal("opening spawn form returned an unexpected command")
	}
	got := next.(agentDashboardModel)
	if got.mode != dashAgentsSpawn {
		t.Fatalf("mode = %v, want dashAgentsSpawn", got.mode)
	}
	if got.pathInput.Value() != "/repo" {
		t.Fatalf("spawn path = %q, want /repo", got.pathInput.Value())
	}
	if !got.taskInput.Focused() {
		t.Fatal("task input should be focused")
	}
}

func TestAgentDashboardOpensConfiguredDirectoryPicker(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	for _, path := range []string{alpha, beta} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	model := newAgentDashboardModel(nil, &config.Config{Directories: []string{alpha, beta}}, nil, root)
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd != nil {
		t.Fatal("opening the directory picker returned an unexpected command")
	}
	got := next.(agentDashboardModel)
	if got.mode != dashAgentsOpenDirectory {
		t.Fatalf("mode = %v, want dashAgentsOpenDirectory", got.mode)
	}
	if !got.directoryInput.Focused() {
		t.Fatal("directory filter should be focused")
	}
	if len(got.filteredDirectories) != 2 {
		t.Fatalf("directory count = %d, want 2", len(got.filteredDirectories))
	}
}

func TestAgentDashboardFiltersAndChoosesDirectory(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	for _, path := range []string{alpha, beta} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	model := newAgentDashboardModel(nil, &config.Config{Directories: []string{alpha, beta}}, nil, root)
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	got := next.(agentDashboardModel)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("beta")})
	got = next.(agentDashboardModel)
	if len(got.filteredDirectories) != 1 || got.filteredDirectories[0] != beta {
		t.Fatalf("filtered directories = %#v, want beta", got.filteredDirectories)
	}
	next, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(agentDashboardModel)
	if cmd == nil {
		t.Fatal("choosing a directory did not quit the dashboard")
	}
	if got.openDirectory != beta {
		t.Fatalf("open directory = %q, want %q", got.openDirectory, beta)
	}
}

func TestAgentDashboardDirectoryPickerMovesAndStaysBounded(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	model.mode = dashAgentsOpenDirectory
	model.width = 58
	model.height = 14
	model.directories = []string{"/repo/alpha", "/repo/beta", "/repo/gamma"}
	model.filteredDirectories = append([]string(nil), model.directories...)
	model.directoryInput.Focus()

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := next.(agentDashboardModel)
	if got.directoryCursor != 1 {
		t.Fatalf("directory cursor = %d, want 1", got.directoryCursor)
	}
	view := got.View()
	if !strings.Contains(view, "beta") {
		t.Fatalf("selected directory missing from picker: %q", view)
	}
	if !strings.Contains(view, "enter") {
		t.Fatalf("picker controls missing at compact height: %q", view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) > got.height {
		t.Fatalf("picker rendered %d lines for height %d", len(lines), got.height)
	}
	for index, line := range lines {
		if width := lipgloss.Width(line); width > got.width {
			t.Fatalf("line %d width = %d, want <= %d", index, width, got.width)
		}
	}
}

func TestAgentDashboardShowsOpenShortcut(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	model.width = 100
	model.height = 20
	view := model.View()
	if !strings.Contains(view, "o open") {
		t.Fatalf("dashboard footer is missing open shortcut: %q", view)
	}
	model.mode = dashAgentsHelp
	if !strings.Contains(model.View(), "Open a configured directory") {
		t.Fatal("dashboard help is missing the open shortcut")
	}
}

func TestEnsureDirectorySessionCreatesThenReusesSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("SHELL", "/bin/sh")
	session := fmt.Sprintf("tsp-open-test-%d", time.Now().UnixNano())
	directory := filepath.Join(t.TempDir(), session)
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tmuxpkg.KillSession(session)
	})

	name, created, err := ensureDirectorySession(directory, "sleep 5")
	if err != nil {
		t.Fatalf("ensureDirectorySession() error = %v", err)
	}
	if name != session || !created {
		t.Fatalf("first open = (%q, %t), want (%q, true)", name, created, session)
	}

	name, created, err = ensureDirectorySession(directory, "sleep 5")
	if err != nil {
		t.Fatalf("ensureDirectorySession() reuse error = %v", err)
	}
	if name != session || created {
		t.Fatalf("second open = (%q, %t), want (%q, false)", name, created, session)
	}
}

func TestAgentDashboardChoosesSpawnAgentAndBaseBranch(t *testing.T) {
	cfg := &config.Config{Spawn: config.SpawnConfig{
		AgentCommand:  "claude --dangerously-skip-permissions",
		ClaudeCommand: "claude --dangerously-skip-permissions",
		CodexCommand:  "codex --full-auto",
	}}
	model := newAgentDashboardModel(nil, cfg, nil, t.TempDir())
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got := next.(agentDashboardModel)

	for range 2 {
		next, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
		got = next.(agentDashboardModel)
	}
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRight})
	got = next.(agentDashboardModel)
	if got.spawnAgent != config.AgentCodex || got.spawnAgentCommand() != "codex --full-auto" {
		t.Fatalf("spawn selection = %s/%s, want codex/codex --full-auto", got.spawnAgent, got.spawnAgentCommand())
	}
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = next.(agentDashboardModel)
	if !got.baseInput.Focused() {
		t.Fatal("base branch input should be focused")
	}
	got.baseInput.SetValue("release/next")
	view := got.renderSpawn()
	if !strings.Contains(view, "CODEX") || !strings.Contains(view, "release/next") {
		t.Fatalf("spawn form did not render the selected agent and base branch: %q", view)
	}
}

func TestAgentDashboardAttachesToSelectedPane(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{{
		run:           service.AgentRun{SessionName: "shared-session", PaneIndex: 3},
		sessionExists: true,
	}}, &config.Config{}, nil, "/repo")
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(agentDashboardModel)
	if got.attachSession != "shared-session" || got.attachPane.Index != 3 {
		t.Fatalf("attach target = %s:%d, want shared-session:3", got.attachSession, got.attachPane.Index)
	}
}

func TestAgentDashboardDoesNotSelectPaneForCompletedLegacyChild(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{{
		run: service.AgentRun{
			ID: "child", ParentRunID: "root", SessionName: "shared-session", PaneIndex: 1, Status: "stopped", Managed: true,
		},
		sessionExists: true,
	}}, &config.Config{}, nil, "/repo")
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(agentDashboardModel)
	if got.attachSession != "shared-session" || got.attachPane.Index != -1 {
		t.Fatalf("completed child attach target = %s:%d", got.attachSession, got.attachPane.Index)
	}
}

func TestRemoveDelegatedRunDoesNotKillPaneForStoppedLegacyChild(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("SHELL", "/bin/sh")
	session := fmt.Sprintf("tsp-legacy-clean-test-%d", time.Now().UnixNano())
	if err := tmuxpkg.CreateTwoPaneSession(session, t.TempDir(), "sleep 5", ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tmuxpkg.KillSession(session)
	})
	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	registryRoot, err := registry.RegisterManaged(service.SpawnResult{Session: session}, service.AgentProviderCodex, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := registry.RegisterDelegated(service.SpawnResult{Session: session}, service.AgentProviderClaude, registryRoot.ID, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.MarkUnseenStopped(map[string]bool{registryRoot.ID: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	legacy, _ = registry.Find(legacy.ID)
	if err := removeDelegatedRun(registry, legacy); err != nil {
		t.Fatal(err)
	}
	if len(tmuxpkg.Panes(session)) != 2 {
		t.Fatal("legacy cleanup killed the pane that reused its index")
	}
}

func TestProviderFromCommand(t *testing.T) {
	tests := map[string]string{
		"claude --dangerously-skip-permissions": service.AgentProviderClaude,
		"codex --full-auto":                     service.AgentProviderCodex,
		"aider":                                 "aider",
		"":                                      service.AgentProviderFallback,
	}
	for command, want := range tests {
		if got := providerFromCommand(command); got != want {
			t.Errorf("providerFromCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestAgentDashboardOpensDelegateForRetainedWorkspace(t *testing.T) {
	cfg := &config.Config{Manager: config.ManagerConfig{
		DefaultAgent: config.AgentClaude,
		Claude:       config.ManagerAgentConfig{Command: "claude -p", Model: "haiku"},
		Codex:        config.ManagerAgentConfig{Command: "codex exec", Model: "gpt-5.6-luna"},
	}}
	model := newAgentDashboardModel([]agentEntry{{
		run: service.AgentRun{
			ID:           "run-parent",
			Task:         "implement the feature",
			SessionName:  "feature-session",
			WorktreePath: "/work/feature",
			CWD:          "/work/feature",
			Managed:      true,
		},
		worktreePath:  "/work/feature",
		sessionExists: true,
	}}, cfg, nil, "/repo")

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Fatal("opening delegate form returned an unexpected command")
	}
	got := next.(agentDashboardModel)
	if got.mode != dashAgentsDelegate {
		t.Fatalf("mode = %v, want dashAgentsDelegate", got.mode)
	}
	if !got.taskInput.Focused() {
		t.Fatal("delegation task input should be focused")
	}
	if got.managerAgent != config.AgentClaude || got.modelInput.Value() != "haiku" {
		t.Fatalf("manager defaults = %s/%s, want claude/haiku", got.managerAgent, got.modelInput.Value())
	}
}

func TestAgentDashboardChoosesManagerAgentAndModel(t *testing.T) {
	cfg := &config.Config{Manager: config.ManagerConfig{
		DefaultAgent: config.AgentClaude,
		Claude:       config.ManagerAgentConfig{Command: "claude -p", Model: "haiku"},
		Codex:        config.ManagerAgentConfig{Command: "codex exec", Model: "gpt-5.6-luna"},
	}}
	model := newAgentDashboardModel([]agentEntry{{
		run:           service.AgentRun{SessionName: "feature", CWD: "/repo"},
		sessionExists: true,
	}}, cfg, nil, "/repo")

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := next.(agentDashboardModel)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = next.(agentDashboardModel)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRight})
	got = next.(agentDashboardModel)
	if got.managerAgent != config.AgentCodex || got.modelInput.Value() != "gpt-5.6-luna" {
		t.Fatalf("manager selection = %s/%s, want codex/gpt-5.6-luna", got.managerAgent, got.modelInput.Value())
	}
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = next.(agentDashboardModel)
	if !got.modelInput.Focused() {
		t.Fatal("model input should be focused")
	}
	view := got.renderDelegate()
	if !strings.Contains(view, "CODEX") || !strings.Contains(view, "gpt-5.6-luna") {
		t.Fatalf("delegate form did not render the selected agent and model: %q", view)
	}
}

func TestAgentDashboardAllowsDelegationWhileParentRuns(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{{
		run: service.AgentRun{
			ID:           "run-parent",
			Task:         "implement the feature",
			SessionName:  "feature-session",
			WorktreePath: "/work/feature",
			CWD:          "/work/feature",
			Managed:      true,
		},
		worktreePath:  "/work/feature",
		live:          true,
		sessionExists: true,
	}}, &config.Config{}, nil, "/repo")

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Fatal("opening manager task returned an unexpected command")
	}
	got := next.(agentDashboardModel)
	got.taskInput.SetValue("make CI green")
	next, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("delegation did not return a command")
	}
	got = next.(agentDashboardModel)
	if !got.busy {
		t.Fatal("dashboard should be busy while the delegated child starts")
	}
	if got.statusMessage != "Starting delegated agent…" {
		t.Fatalf("status message = %q", got.statusMessage)
	}
}

func TestAgentDashboardRoutesCleanupLanguageToConfirmation(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{{
		run: service.AgentRun{
			ID:           "run-parent",
			Task:         "finished task",
			SessionName:  "feature-session",
			WorktreePath: "/work/feature",
			CWD:          "/work/feature",
			Managed:      true,
		},
		worktreePath:  "/work/feature",
		sessionExists: true,
	}}, &config.Config{}, nil, "/repo")

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := next.(agentDashboardModel)
	got.taskInput.SetValue("delete this worktree")
	next, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("cleanup routing returned an unexpected command before confirmation")
	}
	got = next.(agentDashboardModel)
	if got.mode != dashAgentsConfirmCleanup {
		t.Fatalf("mode = %v, want dashAgentsConfirmCleanup", got.mode)
	}
	if !strings.Contains(got.statusMessage, "confirmed TSP action") {
		t.Fatalf("status message = %q", got.statusMessage)
	}
}

func TestClassifyManagerTask(t *testing.T) {
	tests := []struct {
		task string
		want managerTaskIntent
	}{
		{task: "make sure CI is green", want: managerIntentDelegate},
		{task: "remove the dead code", want: managerIntentDelegate},
		{task: "clean up the failing tests", want: managerIntentDelegate},
		{task: "delete the stale feature branch after merging", want: managerIntentDelegate},
		{task: "delete this worktree", want: managerIntentCleanup},
		{task: "remove the agent workspace", want: managerIntentCleanup},
		{task: "stop this agent", want: managerIntentStop},
		{task: "kill the current process", want: managerIntentStop},
	}
	for _, test := range tests {
		t.Run(test.task, func(t *testing.T) {
			if got := classifyManagerTask(test.task); got != test.want {
				t.Fatalf("classifyManagerTask(%q) = %v, want %v", test.task, got, test.want)
			}
		})
	}
}

func TestBuildDelegationPromptIncludesDurableContextAndSafetyBoundary(t *testing.T) {
	parent := agentEntry{
		run: service.AgentRun{
			ID:           "run-parent",
			Provider:     service.AgentProviderCodex,
			Task:         "replace the dashboard",
			CWD:          "/work/dashboard",
			WorktreePath: "/work/dashboard",
			Branch:       "spawn/dashboard",
		},
		branch:       "spawn/dashboard",
		worktreePath: "/work/dashboard",
		output:       "tests failed\nCI job: lint\n",
	}
	prompt := buildDelegationPrompt(parent, "make CI green")
	for _, expected := range []string{
		"Target workspace: /work/dashboard",
		"Target branch: spawn/dashboard",
		"Parent run: run-parent",
		"Original task: replace the dashboard",
		"CI job: lint",
		"make CI green",
		"do not delete, move, or unregister the target worktree or branch",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("delegation prompt missing %q", expected)
		}
	}
}

func TestSortAgentEntriesKeepsDelegationsUnderParent(t *testing.T) {
	now := time.Now()
	root := agentEntry{run: service.AgentRun{ID: "root", Task: "root", Managed: true, StartedAt: now}}
	child := agentEntry{run: service.AgentRun{ID: "child", ParentRunID: "root", Task: "child", Managed: true, StartedAt: now.Add(time.Minute)}, live: true}
	other := agentEntry{run: service.AgentRun{ID: "other", Task: "other", Managed: true, StartedAt: now.Add(-time.Minute)}}

	ordered := sortAgentEntries([]agentEntry{child, other, root})
	got := []string{ordered[0].run.ID, ordered[1].run.ID, ordered[2].run.ID}
	want := []string{"root", "child", "other"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ordered ids = %v, want %v", got, want)
		}
	}
}

func TestAgentDashboardSelectsDelegatedChildAfterStart(t *testing.T) {
	parent := agentEntry{run: service.AgentRun{ID: "root", Task: "parent", Managed: true}}
	child := agentEntry{run: service.AgentRun{ID: "child", ParentRunID: "root", Task: "make CI green", Managed: true}}
	model := newAgentDashboardModel([]agentEntry{parent}, &config.Config{}, nil, "/repo")

	next, cmd := model.Update(agentActionDoneMsg{
		agents:     []agentEntry{parent, child},
		message:    "Delegated agent started",
		selectedID: "child",
	})
	if cmd != nil {
		t.Fatal("completed delegation returned an unexpected command")
	}
	got := next.(agentDashboardModel)
	selected, ok := got.selected()
	if !ok || selected.run.ID != "child" {
		t.Fatalf("selected run = %q, want child", selected.run.ID)
	}
	if got.statusMessage != "Delegated agent started" {
		t.Fatalf("status message = %q", got.statusMessage)
	}
}

func TestSortAgentEntriesKeepsCyclicRegistryRowsVisible(t *testing.T) {
	one := agentEntry{run: service.AgentRun{ID: "one", ParentRunID: "two", Task: "one"}}
	two := agentEntry{run: service.AgentRun{ID: "two", ParentRunID: "one", Task: "two"}}
	ordered := sortAgentEntries([]agentEntry{one, two})
	if len(ordered) != 2 {
		t.Fatalf("sortAgentEntries cycle length = %d, want 2", len(ordered))
	}
}

func TestDelegatedRunDoesNotOwnSharedWorkspace(t *testing.T) {
	root := agentEntry{
		run:          service.AgentRun{Managed: true},
		worktreePath: "/work/root",
	}
	child := agentEntry{
		run:          service.AgentRun{Managed: true, ParentRunID: "root"},
		worktreePath: "/work/root",
	}
	if !root.ownsWorkspace() {
		t.Fatal("root managed run should own its workspace")
	}
	if child.ownsWorkspace() {
		t.Fatal("delegated run must not own its shared workspace")
	}
}

func TestOwnsWorkspace(t *testing.T) {
	tests := []struct {
		name  string
		entry agentEntry
		want  bool
	}{
		{
			name:  "managed root run with worktree",
			entry: agentEntry{run: service.AgentRun{Managed: true}, worktreePath: "/tmp/wt"},
			want:  true,
		},
		{
			name:  "observed session inside a worktree",
			entry: agentEntry{worktreePath: "/tmp/wt", isWorktree: true},
			want:  true,
		},
		{
			name:  "observed session in a plain checkout",
			entry: agentEntry{worktreePath: "/tmp/repo"},
			want:  false,
		},
		{
			name:  "delegated child inside a worktree",
			entry: agentEntry{run: service.AgentRun{ParentRunID: "run_parent"}, worktreePath: "/tmp/wt", isWorktree: true},
			want:  false,
		},
		{
			name:  "managed run without a worktree",
			entry: agentEntry{run: service.AgentRun{Managed: true}},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entry.ownsWorkspace(); got != tt.want {
				t.Fatalf("ownsWorkspace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverAgentsMarksWorktreeSessionAsOwningItsWorkspace(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, strings.TrimSpace(string(output)))
		}
	}
	run(repo, "git", "init", "-q", ".")
	run(repo, "git", "commit", "-q", "--allow-empty", "-m", "init")
	worktree := filepath.Join(t.TempDir(), "wt")
	run(repo, "git", "worktree", "add", "-q", "-b", "wt-branch", worktree)

	sessionName := fmt.Sprintf("tsp-worktree-cleanup-test-%d", time.Now().UnixNano())
	create := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", worktree)
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v: %s", err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	entries, err := discoverAgents(registry)
	if err != nil {
		t.Fatalf("discoverAgents: %v", err)
	}
	for _, entry := range entries {
		if entry.run.SessionName != sessionName {
			continue
		}
		if !entry.ownsWorkspace() {
			t.Fatalf("worktree session does not own its workspace: %#v", entry)
		}
		if entry.branch != "wt-branch" {
			t.Fatalf("branch = %q, want %q", entry.branch, "wt-branch")
		}
		return
	}
	t.Fatalf("worktree session %q was omitted", sessionName)
}

func TestCleanupAgentRemovesWorktreeSessionFromDisk(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, strings.TrimSpace(string(output)))
		}
	}
	run(repo, "git", "init", "-q", ".")
	run(repo, "git", "commit", "-q", "--allow-empty", "-m", "init")
	worktree := filepath.Join(t.TempDir(), "wt")
	run(repo, "git", "worktree", "add", "-q", "-b", "wt-cleanup-branch", worktree)

	sessionName := fmt.Sprintf("tsp-worktree-removal-test-%d", time.Now().UnixNano())
	create := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", worktree)
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v: %s", err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	entries, err := discoverAgents(registry)
	if err != nil {
		t.Fatalf("discoverAgents: %v", err)
	}
	var selected agentEntry
	for _, entry := range entries {
		if entry.run.SessionName == sessionName {
			selected = entry
		}
	}
	if selected.run.SessionName == "" {
		t.Fatalf("worktree session %q was omitted", sessionName)
	}

	done, ok := cleanupAgentCmd(registry, selected)().(agentActionDoneMsg)
	if !ok {
		t.Fatal("cleanupAgentCmd did not return agentActionDoneMsg")
	}
	if done.err != nil {
		t.Fatalf("cleanup failed: %v", done.err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree directory still on disk: %v", err)
	}
	if exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/wt-cleanup-branch").Run() == nil {
		t.Fatal("worktree branch still exists")
	}
	if tmuxpkg.SessionExists(sessionName) {
		t.Fatal("tmux session still exists")
	}
}

func TestDiscoverAgentsKeepsPlainRepoSessionUnowned(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	repo := t.TempDir()
	init := exec.Command("git", "init", "-q", ".")
	init.Dir = repo
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, strings.TrimSpace(string(output)))
	}

	sessionName := fmt.Sprintf("tsp-plain-repo-test-%d", time.Now().UnixNano())
	create := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", repo)
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v: %s", err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	entries, err := discoverAgents(registry)
	if err != nil {
		t.Fatalf("discoverAgents: %v", err)
	}
	for _, entry := range entries {
		if entry.run.SessionName != sessionName {
			continue
		}
		if entry.ownsWorkspace() {
			t.Fatalf("plain checkout session claims workspace ownership: %#v", entry)
		}
		resolved, err := filepath.EvalSymlinks(repo)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if entry.workspacePath() != resolved {
			t.Fatalf("workspacePath() = %q, want %q", entry.workspacePath(), resolved)
		}
		return
	}
	t.Fatalf("plain repo session %q was omitted", sessionName)
}
