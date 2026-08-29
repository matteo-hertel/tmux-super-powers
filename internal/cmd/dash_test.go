package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

func TestAgentDashboardDoesNotPoll(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	if cmd := model.Init(); cmd != nil {
		t.Fatal("dashboard Init returned a command; snapshots must refresh only on demand")
	}
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
	if got.attachSession != "shared-session" || got.attachPane != 3 {
		t.Fatalf("attach target = %s:%d, want shared-session:3", got.attachSession, got.attachPane)
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
