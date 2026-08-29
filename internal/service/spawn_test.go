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

func TestSpawnCopyNodeModules(t *testing.T) {
	repoRoot := t.TempDir()
	worktree := t.TempDir()

	// Create a fake node_modules in repo root
	nmDir := filepath.Join(repoRoot, "node_modules", "fake-pkg")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(nmDir, "index.js")
	if err := os.WriteFile(testFile, []byte("module.exports = 42"), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy
	err := spawnCopyNodeModules(repoRoot, worktree)
	if err != nil {
		t.Fatalf("spawnCopyNodeModules failed: %v", err)
	}

	// Verify file was hardlinked (same inode)
	srcInfo, _ := os.Stat(testFile)
	dstFile := filepath.Join(worktree, "node_modules", "fake-pkg", "index.js")
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatalf("destination file not found: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Error("expected hardlink (same inode), got different files")
	}

	// Verify content
	content, _ := os.ReadFile(dstFile)
	if string(content) != "module.exports = 42" {
		t.Errorf("content mismatch: %s", content)
	}
}

func TestSpawnCopyNodeModulesNoNodeModules(t *testing.T) {
	repoRoot := t.TempDir()
	worktree := t.TempDir()

	err := spawnCopyNodeModules(repoRoot, worktree)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
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
	if _, err := SpawnDelegatedAgent("task", "prompt", "", "", 1, "", "", "claude -p"); err == nil {
		t.Fatal("SpawnDelegatedAgent accepted an empty workspace")
	}

	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SpawnDelegatedAgent("task", "prompt", filePath, "parent", 1, "", "", "claude -p"); err == nil {
		t.Fatal("SpawnDelegatedAgent accepted a file as its workspace")
	}

	if _, err := SpawnDelegatedAgent("task", "prompt", t.TempDir(), "parent", 1, "", "", ""); err == nil {
		t.Fatal("SpawnDelegatedAgent accepted an empty manager command")
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
	create := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", t.TempDir())
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v: %s", err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() {
		_ = tmuxpkg.KillSession(sessionName)
	})

	result, err := SpawnDelegatedAgent(
		"check CI", "check CI", t.TempDir(), sessionName, 99, "main", "", "sleep 30 #",
	)
	if err != nil {
		t.Fatalf("SpawnDelegatedAgent: %v", err)
	}
	if result.Session != sessionName {
		t.Fatalf("delegated session = %q, want %q", result.Session, sessionName)
	}
	if result.PaneIndex == 0 || !tmuxpkg.PaneExists(sessionName, result.PaneIndex) {
		t.Fatalf("delegated pane %d does not exist in parent session", result.PaneIndex)
	}
}
