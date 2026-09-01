package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

func TestTaskToBranch(t *testing.T) {
	tests := []struct {
		task string
		want string
	}{
		{"fix the auth bug", "spawn/fix-the-auth-bug"},
		{"", "spawn/task"},
		{"Add Dark Mode!!!", "spawn/add-dark-mode"},
		{"UPPERCASE task", "spawn/uppercase-task"},
		{"special chars: @#$%", "spawn/special-chars"},
		{"multiple   spaces", "spawn/multiple-spaces"},
	}
	for _, tt := range tests {
		t.Run(tt.task, func(t *testing.T) {
			got := TaskToBranch(tt.task)
			if got != tt.want {
				t.Errorf("TaskToBranch(%q) = %q, want %q", tt.task, got, tt.want)
			}
		})
	}
}

func TestBuildLaunchCommand(t *testing.T) {
	agent := "claude 'fix the bug'"
	fallback := `; else exec "$SHELL"; fi`
	tests := []struct {
		name    string
		install string
		setup   string
		want    string
	}{
		{"no install or setup", "", "", agent},
		{"install only", "yarn install", "", "if ( yarn install ); then " + agent + fallback},
		{"setup only", "", "make deps", "if ( make deps ); then " + agent + fallback},
		{"install then setup", "pnpm install", "make deps", "if ( pnpm install ) && ( make deps ); then " + agent + fallback},
		{"multi-command setup stays grouped", "npm install", "a; b", "if ( npm install ) && ( a; b ); then " + agent + fallback},
		{"blank steps ignored", "  ", "\t", agent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLaunchCommand(tt.install, tt.setup, agent)
			if got != tt.want {
				t.Errorf("buildLaunchCommand(%q, %q) = %q, want %q", tt.install, tt.setup, got, tt.want)
			}
		})
	}
}

func TestTaskToBranchTruncation(t *testing.T) {
	long := "a very long task name that exceeds the fifty character limit for branch names"
	got := TaskToBranch(long)
	branch := got[len("spawn/"):]
	if len(branch) > 50 {
		t.Errorf("branch name too long: %d chars", len(branch))
	}
	if branch[len(branch)-1] == '-' {
		t.Error("branch name should not end with hyphen")
	}
}

func TestSpawnDelegatedAgentValidatesWorkspaceAndCommand(t *testing.T) {
	if _, err := SpawnDelegatedAgent("task", "prompt", "", "", tmuxpkg.Pane{Index: 1}, "", "", "claude -p", "haiku"); err == nil {
		t.Fatal("SpawnDelegatedAgent accepted an empty workspace")
	}

	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SpawnDelegatedAgent("task", "prompt", filePath, "parent", tmuxpkg.Pane{Index: 1}, "", "", "claude -p", "haiku"); err == nil {
		t.Fatal("SpawnDelegatedAgent accepted a file as its workspace")
	}

	if _, err := SpawnDelegatedAgent("task", "prompt", t.TempDir(), "parent", tmuxpkg.Pane{Index: 1}, "", "", "", "haiku"); err == nil {
		t.Fatal("SpawnDelegatedAgent accepted an empty manager command")
	}
}

func TestBuildManagerAgentCommandAddsModelAndQuotesPrompt(t *testing.T) {
	got := BuildManagerAgentCommand("codex exec --ephemeral", "gpt-5.6-luna", "fix Matt's test")
	want := "codex exec --ephemeral --model 'gpt-5.6-luna' 'fix Matt'\\''s test'"
	if got != want {
		t.Fatalf("BuildManagerAgentCommand() = %q, want %q", got, want)
	}
}

func TestBuildAgentCommandQuotesPrompt(t *testing.T) {
	got := BuildAgentCommand("claude -p", "fix Matt's test")
	want := "claude -p 'fix Matt'\\''s test'"
	if got != want {
		t.Fatalf("BuildAgentCommand() = %q, want %q", got, want)
	}
}

func TestSpawnDelegatedAgentSplitsParentSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	sessionName := fmt.Sprintf("tsp-delegate-split-test-%d", time.Now().UnixNano())
	if err := tmuxpkg.CreateTwoPaneSession(sessionName, t.TempDir(), "sleep 5", ""); err != nil {
		t.Fatalf("create tmux session: %v", err)
	}
	t.Cleanup(func() {
		_ = tmuxpkg.KillSession(sessionName)
	})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")

	result, err := SpawnDelegatedAgent(
		"check CI", "check CI", t.TempDir(), sessionName, tmuxpkg.Pane{Index: 99}, "main", "",
		"sh -c 'i=1; while [ $i -le 200 ]; do echo delegate-line-$i; i=$((i + 1)); done' placeholder", "",
	)
	if err != nil {
		t.Fatalf("SpawnDelegatedAgent: %v", err)
	}
	if result.Session != sessionName {
		t.Fatalf("delegated session = %q, want %q", result.Session, sessionName)
	}
	if result.PaneID == "" {
		t.Fatal("delegated pane has no stable ID")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !tmuxpkg.PaneExists(sessionName, tmuxpkg.Pane{ID: result.PaneID, Index: result.PaneIndex}) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if tmuxpkg.PaneExists(sessionName, tmuxpkg.Pane{ID: result.PaneID, Index: result.PaneIndex}) {
		t.Fatal("delegated pane stayed open after the agent exited")
	}
	if !tmuxpkg.PaneExists(sessionName, tmuxpkg.Pane{Index: result.PaneIndex}) {
		t.Fatal("expected the shell pane to reuse the delegated pane index")
	}
	output := ReadStoredAgentOutput(result.OutputPath)
	if !strings.Contains(output, "delegate-line-1") || !strings.Contains(output, "delegate-line-200") {
		t.Fatalf("delegated output was truncated: %q", output)
	}
}
