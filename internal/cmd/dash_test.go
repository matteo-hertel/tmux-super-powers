package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

	view := model.render()
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

	for index, line := range strings.Split(model.render(), "\n") {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("line %d width = %d, want <= %d", index, width, model.width)
		}
	}
}

func TestAgentDashboardRosterRowsDoNotWrap(t *testing.T) {
	long := strings.Repeat("backend-very-long-session-name-", 4)
	model := newAgentDashboardModel([]agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: long}, branch: long, live: true, sessionExists: true},
		{
			run:    service.AgentRun{ID: "run", SessionName: long, Provider: service.AgentProviderClaude, Task: long},
			branch: long, live: true, sessionExists: true,
		},
	}, &config.Config{}, nil, "/repo")
	model.expanded[long] = true

	lines := strings.Split(model.renderRoster(40, 20, false), "\n")
	if !strings.Contains(lines[4], "CLAUDE") {
		t.Fatalf("agent row does not start on line 4, so a row wrapped: %q", lines)
	}
}

func TestAgentDashboardHeaderCountsSessionsAndAgents(t *testing.T) {
	entries := []agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: "agent-session"}, live: true, sessionExists: true},
		{
			run:  service.AgentRun{ID: "run-agent", SessionName: "agent-session", PaneIndex: 1},
			live: true, sessionExists: true,
		},
		{
			run:  service.AgentRun{ID: "run-child", SessionName: "agent-session", PaneIndex: 2},
			live: true, sessionExists: true,
		},
		{isSession: true, run: service.AgentRun{SessionName: "done-session"}, sessionExists: true},
		{
			run:           service.AgentRun{ID: "run-exited", SessionName: "done-session", Managed: true},
			sessionExists: true,
		},
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	model.width = 100
	model.height = 20

	view := model.render()
	if !strings.Contains(view, "2 sessions · 3 agents · 2 running") {
		t.Fatalf("dashboard header does not count sessions and agents: %q", view)
	}
}

func TestAgentDashboardCompactRosterShowsSevenSessions(t *testing.T) {
	var entries []agentEntry
	for index := 0; index < 7; index++ {
		entries = append(entries, agentEntry{
			isSession: true,
			run: service.AgentRun{
				SessionName: fmt.Sprintf("session-%d", index),
			},
			sessionExists: true,
		})
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	model.width = 80
	model.height = 24
	view := model.render()
	if !strings.Contains(view, "session-0") || !strings.Contains(view, "session-6") {
		t.Fatalf("compact roster did not show all seven sessions: %q", view)
	}
}

func TestDiscoverAgentsListsSessionWithoutAgent(t *testing.T) {
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
	var rows []agentEntry
	for _, entry := range entries {
		if entry.run.SessionName == sessionName {
			rows = append(rows, entry)
		}
	}
	if len(rows) != 1 || !rows[0].isSession || rows[0].status() != "idle" {
		t.Fatalf("session rows = %#v, want one idle session row", rows)
	}
}

func TestAgentDashboardOpensSpawnFormWithCurrentProject(t *testing.T) {
	model := newAgentDashboardModel(nil, &config.Config{}, nil, "/repo")
	next, cmd := model.Update(keyPress("n"))
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
	next, cmd := model.Update(keyPress("o"))
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
	next, _ := model.Update(keyPress("o"))
	got := next.(agentDashboardModel)
	next, _ = got.Update(keyPress("beta"))
	got = next.(agentDashboardModel)
	if len(got.filteredDirectories) != 1 || got.filteredDirectories[0] != beta {
		t.Fatalf("filtered directories = %#v, want beta", got.filteredDirectories)
	}
	next, cmd := got.Update(keyPress("enter"))
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

	next, _ := model.Update(keyPress("down"))
	got := next.(agentDashboardModel)
	if got.directoryCursor != 1 {
		t.Fatalf("directory cursor = %d, want 1", got.directoryCursor)
	}
	view := got.render()
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
	view := model.render()
	if !strings.Contains(view, "o open") {
		t.Fatalf("dashboard footer is missing open shortcut: %q", view)
	}
	model.mode = dashAgentsHelp
	if !strings.Contains(model.render(), "Open a configured directory") {
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
	next, _ := model.Update(keyPress("n"))
	got := next.(agentDashboardModel)

	for range 2 {
		next, _ = got.Update(keyPress("tab"))
		got = next.(agentDashboardModel)
	}
	next, _ = got.Update(keyPress("right"))
	got = next.(agentDashboardModel)
	if got.spawnAgent != config.AgentCodex || got.spawnAgentCommand() != "codex --full-auto" {
		t.Fatalf("spawn selection = %s/%s, want codex/codex --full-auto", got.spawnAgent, got.spawnAgentCommand())
	}
	next, _ = got.Update(keyPress("tab"))
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
	next, _ := model.Update(keyPress("enter"))
	got := next.(agentDashboardModel)
	if got.attachSession != "shared-session" || got.attachPane.Index != 3 {
		t.Fatalf("attach target = %s:%d, want shared-session:3", got.attachSession, got.attachPane.Index)
	}
}

func TestGroupBySessionNestsAgentsUnderTheirSession(t *testing.T) {
	sessions := []agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: "alpha"}, sessionExists: true},
		{isSession: true, run: service.AgentRun{SessionName: "idle"}, sessionExists: true, output: "idle prompt"},
	}
	agents := []agentEntry{
		{run: service.AgentRun{ID: "run-gone", SessionName: "gone", Managed: true}, branch: "spawn/gone"},
		{run: service.AgentRun{ID: "run-alpha", SessionName: "alpha"}, live: true, sessionExists: true, output: "alpha output"},
	}

	rows := groupBySession(sessions, agents)
	var got []string
	for _, row := range rows {
		got = append(got, row.key())
	}
	want := "session:alpha,run-alpha,session:idle,session:gone,run-gone"
	if strings.Join(got, ",") != want {
		t.Fatalf("rows = %v, want %s", got, want)
	}
	if !rows[0].live || rows[0].output != "alpha output" {
		t.Fatalf("session row did not take its agent's state: %#v", rows[0])
	}
	if rows[3].status() != "closed" || rows[3].branch != "spawn/gone" {
		t.Fatalf("row for a closed session = %#v", rows[3])
	}
}

func TestAgentDashboardFoldsSessionsWithHAndL(t *testing.T) {
	entries := []agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: "alpha"}, live: true, sessionExists: true},
		{run: service.AgentRun{ID: "run-a", SessionName: "alpha"}, live: true, sessionExists: true},
		{isSession: true, run: service.AgentRun{SessionName: "beta"}, sessionExists: true},
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	press := func(m agentDashboardModel, key string) agentDashboardModel {
		next, _ := m.Update(keyPress(key))
		return next.(agentDashboardModel)
	}
	visible := func(m agentDashboardModel) string {
		var keys []string
		for _, row := range m.rows() {
			keys = append(keys, row.key())
		}
		return strings.Join(keys, ",")
	}

	if got := visible(model); got != "session:alpha,session:beta" {
		t.Fatalf("collapsed rows = %s", got)
	}
	model = press(model, "l")
	if got := visible(model); got != "session:alpha,run-a,session:beta" {
		t.Fatalf("expanded rows = %s", got)
	}
	model = press(model, "j")
	if selected, _ := model.selected(); selected.key() != "run-a" {
		t.Fatalf("selected after j = %q, want run-a", selected.key())
	}
	model = press(model, "h")
	if selected, _ := model.selected(); selected.key() != "session:alpha" || visible(model) != "session:alpha,session:beta" {
		t.Fatalf("h did not fold back to the session: selected %q, rows %s", selected.key(), visible(model))
	}

	next, _ := model.Update(agentActionDoneMsg{agents: entries, message: "Agent spawned", selectedID: "run-a"})
	model = next.(agentDashboardModel)
	if selected, _ := model.selected(); selected.key() != "run-a" {
		t.Fatalf("new agent in a collapsed session was not revealed: %q", selected.key())
	}
}

func filterRosterEntries() []agentEntry {
	return []agentEntry{
		{
			isSession:     true,
			run:           service.AgentRun{SessionName: "alpha", CWD: "/work/alpha"},
			branch:        "main",
			live:          true,
			sessionExists: true,
		},
		{
			run: service.AgentRun{
				ID: "run-alpha", SessionName: "alpha", Provider: service.AgentProviderClaude,
				Task: "rewrite the parser", CWD: "/work/alpha",
			},
			branch: "main", live: true, sessionExists: true,
		},
		{
			isSession:     true,
			run:           service.AgentRun{SessionName: "beta", CWD: "/work/beta"},
			branch:        "spawn/beta",
			sessionExists: true,
		},
		{
			run: service.AgentRun{
				ID: "run-beta", SessionName: "beta", Provider: service.AgentProviderCodex,
				Task: "update the docs", CWD: "/work/beta",
			},
			branch: "spawn/beta", sessionExists: true,
		},
		{
			isSession:     true,
			run:           service.AgentRun{SessionName: "gamma", CWD: "/work/gamma"},
			branch:        "release",
			sessionExists: true,
		},
	}
}

func pressKey(t *testing.T, model agentDashboardModel, key string) (agentDashboardModel, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(keyPress(key))
	return next.(agentDashboardModel), cmd
}

func resizeDashboard(t *testing.T, model agentDashboardModel, width, height int) agentDashboardModel {
	t.Helper()
	next, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(agentDashboardModel)
}

// isQuitCmd reports whether a command asks Bubble Tea to quit without blocking
// on commands that wait, such as the text input's cursor blink.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case msg := <-result:
		_, quit := msg.(tea.QuitMsg)
		return quit
	case <-time.After(50 * time.Millisecond):
		// tea.Quit returns immediately; anything still waiting is not a quit.
		return false
	}
}

func visibleRowKeys(model agentDashboardModel) string {
	var keys []string
	for _, row := range model.rows() {
		keys = append(keys, row.key())
	}
	return strings.Join(keys, ",")
}

func TestAgentDashboardFilterNarrowsRosterToMatchingSessions(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")

	model, cmd := pressKey(t, model, "/")
	if cmd != nil {
		t.Fatal("opening the filter returned an unexpected command")
	}
	if !model.filterFocused || !model.filterInput.Focused() {
		t.Fatal("pressing / did not focus the filter input")
	}
	if got := visibleRowKeys(model); got != "session:alpha,session:beta,session:gamma" {
		t.Fatalf("rows before typing = %s", got)
	}

	model, _ = pressKey(t, model, "parser")
	if got := visibleRowKeys(model); got != "session:alpha" {
		t.Fatalf("rows matching an agent task = %s, want session:alpha only", got)
	}
	if model.filterInput.Value() != "parser" {
		t.Fatalf("filter value = %q", model.filterInput.Value())
	}
}

func TestAgentDashboardFilterEnterAppliesAndEscClearsWithoutQuitting(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model.expanded["alpha"] = true

	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")
	model, cmd := pressKey(t, model, "enter")
	if cmd != nil {
		t.Fatal("applying the filter returned an unexpected command")
	}
	if model.filterFocused || model.filterInput.Focused() {
		t.Fatal("enter should unfocus the filter input")
	}
	if model.filterQuery() != "parser" || !model.filterVisible() {
		t.Fatalf("enter dropped the query: %q visible=%t", model.filterQuery(), model.filterVisible())
	}
	if got := visibleRowKeys(model); got != "session:alpha,run-alpha" {
		t.Fatalf("filtered rows after enter = %s", got)
	}

	model, _ = pressKey(t, model, "j")
	if selected, _ := model.selected(); selected.key() != "run-alpha" {
		t.Fatalf("j inside the filtered roster selected %q", selected.key())
	}
	model, _ = pressKey(t, model, "k")
	if selected, _ := model.selected(); selected.key() != "session:alpha" {
		t.Fatalf("k inside the filtered roster selected %q", selected.key())
	}

	model, cmd = pressKey(t, model, "esc")
	if cmd != nil {
		t.Fatal("esc with an active filter quit the dashboard")
	}
	if model.filterVisible() || model.filterQuery() != "" {
		t.Fatal("esc did not clear the active filter")
	}
	if got := visibleRowKeys(model); got != "session:alpha,run-alpha,session:beta,session:gamma" {
		t.Fatalf("rows after clearing the filter = %s", got)
	}

	model, cmd = pressKey(t, model, "esc")
	if cmd == nil {
		t.Fatal("esc without a filter did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("esc without a filter returned %T, want tea.QuitMsg", cmd())
	}
}

func TestAgentDashboardFilterEscWhileFocusedHidesBar(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")

	model, cmd := pressKey(t, model, "esc")
	if cmd != nil {
		t.Fatal("esc while the filter is focused quit the dashboard")
	}
	if model.filterFocused || model.filterInput.Value() != "" || model.filterVisible() {
		t.Fatalf("esc left the filter bar up: focused=%t value=%q", model.filterFocused, model.filterInput.Value())
	}
	if got := visibleRowKeys(model); got != "session:alpha,session:beta,session:gamma" {
		t.Fatalf("rows after esc = %s", got)
	}
}

func TestAgentDashboardFilterKeepsCursorInsideFilteredRows(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model.cursor = len(model.rows()) - 1
	if selected, _ := model.selected(); selected.key() != "session:gamma" {
		t.Fatalf("cursor did not start on the last row: %q", selected.key())
	}

	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")
	selected, ok := model.selected()
	if !ok {
		t.Fatalf("cursor %d is out of bounds for %d filtered rows", model.cursor, len(model.rows()))
	}
	if model.cursor != 0 || selected.key() != "session:alpha" {
		t.Fatalf("cursor = %d selecting %q, want 0 selecting session:alpha", model.cursor, selected.key())
	}
}

func TestAgentDashboardFilterRefocusesAndReportsSessionCounts(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")
	model, _ = pressKey(t, model, "enter")

	for _, size := range []struct{ width, height int }{{120, 42}, {72, 24}} {
		model.width = size.width
		model.height = size.height
		view := model.render()
		if !strings.Contains(view, "FILTER") {
			t.Fatalf("filter bar missing at %dx%d: %q", size.width, size.height, view)
		}
		if !strings.Contains(view, "SESSIONS  1 / 3") {
			t.Fatalf("roster heading counted filtered-out sessions at %dx%d: %q", size.width, size.height, view)
		}
		if !strings.Contains(view, "/ filter") {
			t.Fatalf("footer is missing the filter shortcut at %dx%d: %q", size.width, size.height, view)
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size.height {
			t.Fatalf("view rendered %d lines for height %d", len(lines), size.height)
		}
		for index, line := range lines {
			if width := lipgloss.Width(line); width > size.width {
				t.Fatalf("line %d width = %d, want <= %d", index, width, size.width)
			}
		}
	}

	refocused, _ := pressKey(t, model, "/")
	if !refocused.filterFocused || refocused.filterQuery() != "parser" {
		t.Fatalf("/ did not refocus the existing query: focused=%t query=%q", refocused.filterFocused, refocused.filterQuery())
	}
	// Lip Gloss v2 always writes color codes; Bubble Tea strips them at output.
	if view := ansi.Strip(refocused.render()); !strings.Contains(view, "enter apply") || !strings.Contains(view, "esc clear") {
		t.Fatalf("focused filter footer = %q", view)
	}
}

func TestAgentDashboardFilterWithoutMatchesEmptiesRosterAndDetail(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model.width = 100
	model.height = 24
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "nothing-matches-this")

	if len(model.rows()) != 0 {
		t.Fatalf("rows = %s, want none", visibleRowKeys(model))
	}
	if detail := model.renderDetail(60, 16); detail != "" {
		t.Fatalf("detail pane rendered for an empty roster: %q", detail)
	}
	if view := model.render(); !strings.Contains(view, "No sessions match this filter.") {
		t.Fatalf("empty filter result is not explained: %q", view)
	}
}

func TestAgentDashboardFocusedFilterSwallowsBrowseShortcuts(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model, _ = pressKey(t, model, "/")

	typed := ""
	for _, key := range []string{"q", "n", "x", "s", "r", "?", "o", "d", "j", "k", "h", "l", "/", " "} {
		var cmd tea.Cmd
		model, cmd = pressKey(t, model, key)
		typed += key
		if isQuitCmd(cmd) {
			t.Fatalf("%q quit the dashboard while the filter was focused", key)
		}
		if model.mode != dashAgentsBrowse {
			t.Fatalf("%q opened mode %v while the filter was focused", key, model.mode)
		}
		if !model.filterFocused {
			t.Fatalf("%q unfocused the filter", key)
		}
		if model.filterInput.Value() != typed {
			t.Fatalf("after %q the filter holds %q, want %q", key, model.filterInput.Value(), typed)
		}
	}
	if model.busy || model.attachSession != "" || model.openDirectory != "" {
		t.Fatalf("typing into the filter triggered a roster action: %#v", model.statusMessage)
	}
}

func TestAgentDashboardFocusedFilterStillQuitsOnCtrlC(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")

	_, cmd := pressKey(t, model, "ctrl+c")
	if !isQuitCmd(cmd) {
		t.Fatal("ctrl+c while the filter was focused did not quit")
	}
}

func TestAgentDashboardFilteredRosterFoldsWithoutLosingSelection(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")
	model, _ = pressKey(t, model, "enter")

	model, _ = pressKey(t, model, "l")
	selected, ok := model.selected()
	if !ok || selected.key() != "session:alpha" {
		t.Fatalf("expanding a filtered session selected %q (ok=%t)", selected.key(), ok)
	}
	if got := visibleRowKeys(model); got != "session:alpha,run-alpha" {
		t.Fatalf("expanded filtered rows = %s", got)
	}

	model, _ = pressKey(t, model, "h")
	selected, ok = model.selected()
	if !ok || selected.key() != "session:alpha" {
		t.Fatalf("folding a filtered session selected %q (ok=%t)", selected.key(), ok)
	}
	if got := visibleRowKeys(model); got != "session:alpha" {
		t.Fatalf("folded filtered rows = %s", got)
	}
}

func TestAgentDashboardRefreshKeepsActiveFilter(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "parser")
	model, _ = pressKey(t, model, "enter")

	model, cmd := pressKey(t, model, "r")
	if cmd == nil || !model.busy {
		t.Fatal("r did not start a refresh")
	}
	next, _ := model.Update(agentsRefreshedMsg{agents: filterRosterEntries()})
	model = next.(agentDashboardModel)

	if model.busy {
		t.Fatal("refresh left the dashboard busy")
	}
	if model.filterQuery() != "parser" || !model.filterVisible() {
		t.Fatalf("refresh dropped the filter: query=%q visible=%t", model.filterQuery(), model.filterVisible())
	}
	if got := visibleRowKeys(model); got != "session:alpha" {
		t.Fatalf("rows after refresh = %s, want the filtered roster", got)
	}
	if selected, ok := model.selected(); !ok || selected.key() != "session:alpha" {
		t.Fatalf("refresh left the cursor on %q (ok=%t)", selected.key(), ok)
	}
}

func TestAgentDashboardFilterScrollsLongQueryIntoView(t *testing.T) {
	model := newAgentDashboardModel(filterRosterEntries(), &config.Config{}, nil, "/repo")
	model = resizeDashboard(t, model, 60, 24)
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, strings.Repeat("alpha-", 10)+"TAIL")

	bar := model.renderFilterBar()
	if !strings.Contains(bar, "TAIL") {
		t.Fatalf("the end of a long query is off-screen at 60 columns: %q", bar)
	}
	if width := lipgloss.Width(bar); width > 60 {
		t.Fatalf("filter bar width = %d, want <= 60", width)
	}
	for index, line := range strings.Split(model.render(), "\n") {
		if width := lipgloss.Width(line); width > 60 {
			t.Fatalf("line %d width = %d, want <= 60", index, width)
		}
	}
}

func TestAgentDashboardFilterHeadingCombinesScrollRangeAndTotal(t *testing.T) {
	var entries []agentEntry
	for index := 0; index < 12; index++ {
		entries = append(entries, agentEntry{
			isSession:     true,
			run:           service.AgentRun{SessionName: fmt.Sprintf("keep-%02d", index)},
			sessionExists: true,
		})
	}
	for index := 0; index < 18; index++ {
		entries = append(entries, agentEntry{
			isSession:     true,
			run:           service.AgentRun{SessionName: fmt.Sprintf("other-%02d", index)},
			sessionExists: true,
		})
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	model = resizeDashboard(t, model, 120, 24)

	if view := model.render(); !strings.Contains(view, "SESSIONS  1–6 / 30") {
		t.Fatalf("unfiltered scrolling heading changed: %q", view)
	}

	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "keep")
	if view := model.render(); !strings.Contains(view, "SESSIONS  1–6 / 12 of 30") {
		t.Fatalf("scrolling filtered heading lost the session total: %q", view)
	}

	model, _ = pressKey(t, model, "-1")
	if view := model.render(); !strings.Contains(view, "SESSIONS  2 / 30") {
		t.Fatalf("non-scrolling filtered heading = %q", view)
	}
}

func TestAgentDashboardHeadingAndHeaderShareSessionDenominator(t *testing.T) {
	entries := []agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: "alpha-live"}, live: true, sessionExists: true},
		{isSession: true, run: service.AgentRun{SessionName: "beta-live"}, sessionExists: true},
		{isSession: true, run: service.AgentRun{SessionName: "gamma-closed"}},
		{
			run: service.AgentRun{
				ID: "retained", SessionName: "gamma-closed", Task: "leftover work", Managed: true,
			},
		},
	}
	model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
	model = resizeDashboard(t, model, 110, 24)
	model, _ = pressKey(t, model, "/")
	model, _ = pressKey(t, model, "alpha-live")

	view := model.render()
	if !strings.Contains(view, "2 sessions · 1 agents · 0 running") {
		t.Fatalf("header stopped counting only open sessions: %q", view)
	}
	if !strings.Contains(view, "SESSIONS  1 / 2") {
		t.Fatalf("roster heading counts a closed session the header ignores: %q", view)
	}
}

func TestAgentDashboardKeepsFooterVisibleAtShortHeights(t *testing.T) {
	entries := []agentEntry{
		{
			isSession:     true,
			run:           service.AgentRun{SessionName: "alpha", CWD: "/work/alpha"},
			branch:        "main",
			live:          true,
			sessionExists: true,
			output:        strings.Repeat("alpha pane output\n", 60),
		},
		{
			run: service.AgentRun{
				ID: "run-alpha", SessionName: "alpha", Provider: service.AgentProviderClaude,
				Task: "rewrite the parser", CWD: "/work/alpha",
			},
			branch: "main", live: true, sessionExists: true,
			output: strings.Repeat("agent output line\n", 60),
		},
		{
			isSession:     true,
			run:           service.AgentRun{SessionName: "beta", CWD: "/work/beta"},
			branch:        "spawn/beta",
			sessionExists: true,
		},
	}

	bounded := func(t *testing.T, model agentDashboardModel, width, height int, footer string) {
		t.Helper()
		lines := strings.Split(model.render(), "\n")
		if len(lines) > height {
			t.Fatalf("view rendered %d lines for height %d", len(lines), height)
		}
		for index, line := range lines {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("line %d width = %d, want <= %d", index, got, width)
			}
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, footer) {
			t.Fatalf("footer %q was pushed off a %dx%d terminal, last line = %q", footer, width, height, last)
		}
	}

	for _, width := range []int{120, 72} {
		for _, height := range []int{14, 12, 10} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				model := newAgentDashboardModel(entries, &config.Config{}, nil, "/repo")
				model = resizeDashboard(t, model, width, height)
				model, _ = pressKey(t, model, "/")
				model, _ = pressKey(t, model, "parser")
				bounded(t, model, width, height, "apply")

				applied, _ := pressKey(t, model, "enter")
				applied, _ = pressKey(t, applied, "l")
				applied, _ = pressKey(t, applied, "j")
				if selected, ok := applied.selected(); !ok || selected.key() != "run-alpha" {
					t.Fatalf("expected the agent row selected, got %q (ok=%t)", selected.key(), ok)
				}
				bounded(t, applied, width, height, "o open")
			})
		}
	}
}

func TestClampRenderedLines(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		height int
		want   []string
	}{
		{name: "fits", lines: []string{"a", "b"}, height: 3, want: []string{"a", "b"}},
		{name: "trims the overflow", lines: []string{"a", "b", "c"}, height: 2, want: []string{"a", "b"}},
		{name: "drops a tall entry whole", lines: []string{"a", "b\nc\nd"}, height: 3, want: []string{"a"}},
		{name: "keeps a tall entry that fits", lines: []string{"a\nb", "c"}, height: 3, want: []string{"a\nb", "c"}},
		{name: "no room at all", lines: []string{"a"}, height: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampRenderedLines(tt.lines, tt.height)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("clampRenderedLines(%q, %d) = %q, want %q", tt.lines, tt.height, got, tt.want)
			}
		})
	}
}

func TestEntryMatchesFilter(t *testing.T) {
	session := agentEntry{
		isSession:     true,
		run:           service.AgentRun{SessionName: "alpha", CWD: "/work/alpha"},
		branch:        "spawn/parser",
		sessionExists: true,
	}
	agent := agentEntry{
		run: service.AgentRun{
			ID: "run-alpha", SessionName: "alpha", Provider: service.AgentProviderCodex,
			Task: "Rewrite the parser", CWD: "/work/alpha",
		},
		branch: "main",
	}

	tests := []struct {
		name  string
		entry agentEntry
		query string
		want  bool
	}{
		{name: "empty query matches a session", entry: session, query: "", want: true},
		{name: "blank query matches an agent", entry: agent, query: "   ", want: true},
		{name: "session name", entry: session, query: "ALP", want: true},
		{name: "session branch", entry: session, query: "spawn/", want: true},
		{name: "session cwd", entry: session, query: "/work/", want: true},
		{name: "session ignores its provider label", entry: session, query: "session", want: false},
		{name: "agent task is case-insensitive", entry: agent, query: "rewrite the parser", want: true},
		{name: "agent provider", entry: agent, query: "codex", want: true},
		{name: "agent branch", entry: agent, query: "main", want: true},
		{name: "agent session name", entry: agent, query: "alpha", want: true},
		{name: "no match", entry: agent, query: "gamma", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entryMatchesFilter(tt.entry, tt.query); got != tt.want {
				t.Fatalf("entryMatchesFilter(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}

	if !sessionMatchesFilter(session, []agentEntry{agent}, "rewrite") {
		t.Fatal("a session with a matching agent must stay visible")
	}
	if sessionMatchesFilter(session, []agentEntry{agent}, "gamma") {
		t.Fatal("a session without any match must be filtered out")
	}
}

func TestAgentDashboardSessionRowAttachesWithoutPaneAndCannotStop(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: "alpha"}, live: true, sessionExists: true},
	}, &config.Config{}, nil, "/repo")

	next, _ := model.Update(keyPress("s"))
	got := next.(agentDashboardModel)
	if got.mode != dashAgentsBrowse {
		t.Fatalf("stop on a session row opened mode %v", got.mode)
	}
	next, _ = got.Update(keyPress("enter"))
	got = next.(agentDashboardModel)
	if got.attachSession != "alpha" || got.attachPane.Index != -1 {
		t.Fatalf("attach target = %s:%d, want alpha with no pane", got.attachSession, got.attachPane.Index)
	}
}

func TestAgentDashboardClosedSessionDoesNotAdvertiseOrSilentlyIgnoreAttach(t *testing.T) {
	model := newAgentDashboardModel([]agentEntry{
		{isSession: true, run: service.AgentRun{SessionName: "closed"}},
		{run: service.AgentRun{ID: "retained", SessionName: "closed", Managed: true}},
	}, &config.Config{}, nil, "/repo")
	model.width = 100
	model.height = 24

	if view := model.renderDetail(60, 16); strings.Contains(view, "attach") || !strings.Contains(view, "CLOSED") {
		t.Fatalf("closed session detail has misleading controls or state: %q", view)
	}

	next, _ := model.Update(keyPress("enter"))
	got := next.(agentDashboardModel)
	if got.attachSession != "" {
		t.Fatalf("closed session set attach target %q", got.attachSession)
	}
	if !strings.Contains(got.statusMessage, "Session is closed") {
		t.Fatalf("closed session Enter message = %q", got.statusMessage)
	}

	model.expanded["closed"] = true
	model.cursor = 1
	if view := model.renderDetail(60, 16); strings.Contains(view, "attach") {
		t.Fatalf("retained agent detail advertises attach after its session closed: %q", view)
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
			name:  "session row inside a worktree",
			entry: agentEntry{isSession: true, worktreePath: "/tmp/wt", isWorktree: true},
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

	sessionName := startFakeAgentSession(t, worktree)

	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	entries, err := discoverAgents(registry)
	if err != nil {
		t.Fatalf("discoverAgents: %v", err)
	}
	for _, entry := range entries {
		if entry.isSession || entry.run.SessionName != sessionName {
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

	sessionName := startFakeAgentSession(t, worktree)

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
		if !entry.isSession && entry.run.SessionName == sessionName {
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

	sessionName := startFakeAgentSession(t, repo)

	registry, err := service.NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	entries, err := discoverAgents(registry)
	if err != nil {
		t.Fatalf("discoverAgents: %v", err)
	}
	for _, entry := range entries {
		if entry.isSession || entry.run.SessionName != sessionName {
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

func startFakeAgentSession(t *testing.T, dir string) string {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not installed")
	}
	agent := filepath.Join(t.TempDir(), "codex")
	if err := os.Symlink(sleep, agent); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/sh")
	session := fmt.Sprintf("tsp-fake-agent-test-%d", time.Now().UnixNano())
	if err := tmuxpkg.CreateTwoPaneSession(session, dir, "'"+agent+"' 30", ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tmuxpkg.KillSession(session)
	})
	pane := tmuxpkg.Pane{Index: 0}
	deadline := time.Now().Add(2 * time.Second)
	for service.DetectPaneAgentProcess(session, pane, service.GetPaneProcess(session, pane)).Provider != service.AgentProviderCodex {
		if time.Now().After(deadline) {
			t.Fatal("fake codex agent was not detected")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return session
}

// keyPress builds the key event Bubble Tea v2 delivers for a key name such as
// "enter" or "ctrl+c", a single character, or a run of typed text.
func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case " ", "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if runes := []rune(key); len(runes) == 1 {
		return tea.KeyPressMsg{Code: runes[0], Text: key}
	}
	return tea.KeyPressMsg{Code: tea.KeyExtended, Text: key}
}
