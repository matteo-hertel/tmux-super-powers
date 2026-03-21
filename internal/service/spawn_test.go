package service

import (
	"os"
	"path/filepath"
	"testing"
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
