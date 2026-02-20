package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPackageManager_BunLockb(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "bun" {
		t.Errorf("detectPackageManager() = %q, want \"bun\"", got)
	}
}

func TestDetectPackageManager_BunLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bun.lock"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "bun" {
		t.Errorf("detectPackageManager() = %q, want \"bun\"", got)
	}
}

func TestDetectPackageManager_Pnpm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "pnpm" {
		t.Errorf("detectPackageManager() = %q, want \"pnpm\"", got)
	}
}

func TestDetectPackageManager_Yarn(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "yarn" {
		t.Errorf("detectPackageManager() = %q, want \"yarn\"", got)
	}
}

func TestDetectPackageManager_Npm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "npm" {
		t.Errorf("detectPackageManager() = %q, want \"npm\"", got)
	}
}

func TestDetectPackageManager_PackageJsonFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
	got := detectPackageManager(dir)
	if got != "npm" {
		t.Errorf("detectPackageManager() = %q, want \"npm\"", got)
	}
}

func TestDetectPackageManager_None(t *testing.T) {
	dir := t.TempDir()
	got := detectPackageManager(dir)
	if got != "" {
		t.Errorf("detectPackageManager() = %q, want \"\"", got)
	}
}

func TestDetectPackageManager_Priority(t *testing.T) {
	dir := t.TempDir()
	// bun should win over yarn
	os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{}, 0644)
	os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte{}, 0644)
	got := detectPackageManager(dir)
	if got != "bun" {
		t.Errorf("detectPackageManager() = %q, want \"bun\" (priority)", got)
	}
}

func TestShouldIgnoreDir_Hidden(t *testing.T) {
	if !shouldIgnoreDir(".git", map[string]bool{}) {
		t.Error("shouldIgnoreDir(\".git\") = false, want true")
	}
}

func TestShouldIgnoreDir_UserIgnore(t *testing.T) {
	ignores := map[string]bool{"node_modules": true}
	if !shouldIgnoreDir("node_modules", ignores) {
		t.Error("shouldIgnoreDir(\"node_modules\") = false, want true")
	}
}

func TestShouldIgnoreDir_CaseInsensitive(t *testing.T) {
	ignores := map[string]bool{"node_modules": true}
	if !shouldIgnoreDir("Node_Modules", ignores) {
		t.Error("shouldIgnoreDir(\"Node_Modules\") with lowercase key = false, want true")
	}
}

func TestShouldIgnoreDir_Normal(t *testing.T) {
	if shouldIgnoreDir("src", map[string]bool{}) {
		t.Error("shouldIgnoreDir(\"src\") = true, want false")
	}
}

func TestBuildIgnoreSet_Empty(t *testing.T) {
	set := buildIgnoreSet(nil)
	if len(set) != 0 {
		t.Errorf("buildIgnoreSet(nil) length = %d, want 0", len(set))
	}
}

func TestBuildIgnoreSet_WithEntries(t *testing.T) {
	set := buildIgnoreSet([]string{"node_modules", "vendor"})
	if !set["node_modules"] {
		t.Error("expected node_modules in set")
	}
	if !set["vendor"] {
		t.Error("expected vendor in set")
	}
}

func TestParseWorktreesPorcelain(t *testing.T) {
	// Simulates git worktree list --porcelain output
	// First entry is always the main worktree — should be skipped
	input := `worktree /Users/my-user/my-repo
branch refs/heads/main

worktree /Users/my-user/work/my-repo-feature-one
branch refs/heads/feature-one

worktree /Users/my-user/work/my-repo-fix-bug
branch refs/heads/fix-bug
`
	worktrees := parseWorktreesPorcelain(input)

	if len(worktrees) != 2 {
		t.Fatalf("parseWorktreesPorcelain() returned %d worktrees, want 2", len(worktrees))
	}
	if worktrees[0].Branch != "feature-one" {
		t.Errorf("worktrees[0].Branch = %q, want \"feature-one\"", worktrees[0].Branch)
	}
	if worktrees[0].Path != "/Users/my-user/work/my-repo-feature-one" {
		t.Errorf("worktrees[0].Path = %q, want \"/Users/my-user/work/my-repo-feature-one\"", worktrees[0].Path)
	}
	if worktrees[1].Branch != "fix-bug" {
		t.Errorf("worktrees[1].Branch = %q, want \"fix-bug\"", worktrees[1].Branch)
	}
}

func TestParseWorktreesPorcelain_HyphenInMainPath(t *testing.T) {
	// The main worktree path contains hyphens — must still be skipped
	input := `worktree /Users/my-user/my-cool-repo
branch refs/heads/main

worktree /tmp/my-cool-repo-dev
branch refs/heads/dev
`
	worktrees := parseWorktreesPorcelain(input)

	if len(worktrees) != 1 {
		t.Fatalf("parseWorktreesPorcelain() returned %d worktrees, want 1", len(worktrees))
	}
	if worktrees[0].Branch != "dev" {
		t.Errorf("worktrees[0].Branch = %q, want \"dev\"", worktrees[0].Branch)
	}
}

func TestParseWorktreesPorcelain_Empty(t *testing.T) {
	worktrees := parseWorktreesPorcelain("")
	if len(worktrees) != 0 {
		t.Errorf("parseWorktreesPorcelain(\"\") returned %d, want 0", len(worktrees))
	}
}
