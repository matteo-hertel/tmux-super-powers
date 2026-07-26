package cmd

import (
	"fmt"
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
	}}, &config.Config{}, nil, "/repo")

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
