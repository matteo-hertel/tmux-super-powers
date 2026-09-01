package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestKillSessionReturnsBeforeALargeWorktreeIsUnlinked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "agent-wt")
	initRepo(t, repo)
	runGit(t, repo, "worktree", "add", "-q", "-b", "spawn/test", worktree)

	// An agent worktree carries a dependency tree; unlinking it file by file is
	// what used to block the dashboard.
	writeManyFiles(t, filepath.Join(worktree, "node_modules"), 3000)

	start := time.Now()
	if err := KillSession("no-such-tmux-session", true, worktree, "spawn/test", repo); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	elapsed := time.Since(start)

	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree path still occupied on return: %v", err)
	}
	// Unlinking these files in the foreground measures ~700ms; the staged
	// rename measures ~250us and does not grow with the file count.
	if elapsed > 300*time.Millisecond {
		t.Errorf("KillSession took %v — the caller is blocking on the unlink again", elapsed)
	}
	if out := runGit(t, repo, "worktree", "list"); contains(out, worktree) {
		t.Errorf("worktree still registered:\n%s", out)
	}
	if out := runGit(t, repo, "branch", "--list", "spawn/test"); out != "" {
		t.Errorf("branch not deleted: %q", out)
	}
}

func TestKillSessionLeavesNoStagedDirectoryBehind(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "agent-wt")
	initRepo(t, repo)
	runGit(t, repo, "worktree", "add", "-q", "-b", "spawn/test", worktree)
	writeManyFiles(t, filepath.Join(worktree, "node_modules"), 200)

	if err := KillSession("no-such-tmux-session", true, worktree, "spawn/test", repo); err != nil {
		t.Fatalf("KillSession: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if len(stagedDirs(t, root)) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("staged directories survived: %v", stagedDirs(t, root))
}

func stagedDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if len(e.Name()) >= len(WorktreeTrashPrefix) && e.Name()[:len(WorktreeTrashPrefix)] == WorktreeTrashPrefix {
			found = append(found, e.Name())
		}
	}
	return found
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-q", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeManyFiles(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		sub := filepath.Join(dir, "pkg", string(rune('a'+i%26)), filepath.Base(filepath.Join("d", itoa(i))))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "index.js"), []byte("module.exports={}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
