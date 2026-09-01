package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorktreeTrashPathIsASiblingOfTheWorktree(t *testing.T) {
	staged := WorktreeTrashPath("/work/code/backend-tiger-71", time.Unix(0, 1700000000))
	if got, want := filepath.Dir(staged), "/work/code"; got != want {
		t.Fatalf("parent = %q, want %q — a cross-filesystem path makes the rename a copy", got, want)
	}
	if !strings.HasPrefix(filepath.Base(staged), WorktreeTrashPrefix) {
		t.Errorf("base = %q, want the %q prefix so the sweep can find it", filepath.Base(staged), WorktreeTrashPrefix)
	}
	if !strings.Contains(staged, "backend-tiger-71") {
		t.Errorf("staged = %q, want the worktree name retained for diagnosis", staged)
	}
}

func TestWorktreeTrashPathIsUniquePerCall(t *testing.T) {
	first := WorktreeTrashPath("/work/code/wt", time.Unix(0, 1))
	second := WorktreeTrashPath("/work/code/wt", time.Unix(0, 2))
	if first == second {
		t.Fatal("two removals of the same worktree path collided")
	}
}

func TestStageWorktreeForRemovalFreesTheOriginalPath(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "agent-worktree")
	if err := os.MkdirAll(filepath.Join(worktree, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "node_modules", "pkg", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := StageWorktreeForRemoval(worktree)
	if err != nil {
		t.Fatalf("StageWorktreeForRemoval: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("worktree path still occupied after staging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staged, "node_modules", "pkg", "index.js")); err != nil {
		t.Errorf("staged tree lost content: %v", err)
	}
}

func TestStageWorktreeForRemovalRejectsDangerousPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	for _, path := range []string{"", "   ", "/", ".", "..", home} {
		if _, err := StageWorktreeForRemoval(path); err == nil {
			t.Errorf("StageWorktreeForRemoval(%q) = nil error, want a refusal", path)
		}
	}
}

func TestRemoveInBackgroundRefusesAnUnstagedDirectory(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "a-real-checkout")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveInBackground(plain); err == nil {
		t.Fatal("background removal accepted a path without the trash prefix")
	}
	if _, err := os.Stat(plain); err != nil {
		t.Errorf("refused path was removed anyway: %v", err)
	}
}

func TestRemoveInBackgroundDeletesAStagedDirectory(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, WorktreeTrashPrefix+"wt-1")
	if err := os.MkdirAll(filepath.Join(staged, "deep", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "deep", "nested", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveInBackground(staged); err != nil {
		t.Fatalf("RemoveInBackground: %v", err)
	}
	if !eventuallyGone(staged, 10*time.Second) {
		t.Fatal("staged directory was not removed")
	}
}

func TestSweepWorktreeTrashRemovesOnlyStagedLeftovers(t *testing.T) {
	root := t.TempDir()
	leftover := filepath.Join(root, WorktreeTrashPrefix+"orphan-1")
	keep := filepath.Join(root, "backend-some-agent")
	for _, dir := range []string{leftover, keep} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	swept := SweepWorktreeTrash(root)
	if len(swept) != 1 || swept[0] != leftover {
		t.Fatalf("swept = %v, want exactly [%s]", swept, leftover)
	}
	if !eventuallyGone(leftover, 10*time.Second) {
		t.Error("leftover staged directory survived the sweep")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("sweep removed a real worktree: %v", err)
	}
}

func eventuallyGone(path string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
