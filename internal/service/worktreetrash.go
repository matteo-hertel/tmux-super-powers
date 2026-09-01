package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// WorktreeTrashPrefix marks a directory that has been staged for deletion but
// not yet unlinked.
const WorktreeTrashPrefix = ".tsp-trash-"

// WorktreeTrashPath builds the staging path for a worktree. It is a sibling of
// the worktree so the rename stays on one filesystem.
func WorktreeTrashPath(worktreePath string, now time.Time) string {
	worktreePath = filepath.Clean(worktreePath)
	parent := filepath.Dir(worktreePath)
	name := fmt.Sprintf("%s%s-%d", WorktreeTrashPrefix, filepath.Base(worktreePath), now.UnixNano())
	return filepath.Join(parent, name)
}

// StageWorktreeForRemoval renames a worktree out of the way and returns the
// staged path. An agent worktree carries its whole dependency tree, so
// unlinking it takes minutes; renaming it is constant time and frees the
// original path immediately.
func StageWorktreeForRemoval(worktreePath string) (string, error) {
	if err := checkRemovable(worktreePath); err != nil {
		return "", err
	}
	staged := WorktreeTrashPath(worktreePath, time.Now())
	if err := os.Rename(worktreePath, staged); err != nil {
		return "", fmt.Errorf("stage worktree for removal: %w", err)
	}
	return staged, nil
}

// RemoveInBackground unlinks a staged directory in a detached process so the
// caller returns immediately. The process is put in its own group to survive
// the interrupt that stops tsp.
func RemoveInBackground(staged string) error {
	if err := checkRemovable(staged); err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.Base(staged), WorktreeTrashPrefix) {
		return fmt.Errorf("refusing to background-remove %q: not a staged directory", staged)
	}
	cmd := exec.Command("rm", "-rf", staged)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background removal: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		CleanupEmptyParents(filepath.Dir(staged))
	}()
	return nil
}

// SweepWorktreeTrash removes staged directories left behind by a tsp that died
// mid-unlink. Returns the paths it started removing.
func SweepWorktreeTrash(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var swept []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), WorktreeTrashPrefix) {
			continue
		}
		staged := filepath.Join(dir, entry.Name())
		if RemoveInBackground(staged) == nil {
			swept = append(swept, staged)
		}
	}
	return swept
}

// checkRemovable rejects paths that must never be handed to a recursive delete.
func checkRemovable(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("refusing to remove an empty path")
	}
	cleaned := filepath.Clean(path)
	if cleaned == "/" || cleaned == "." || cleaned == ".." {
		return fmt.Errorf("refusing to remove %q", cleaned)
	}
	if home, err := os.UserHomeDir(); err == nil && cleaned == filepath.Clean(home) {
		return fmt.Errorf("refusing to remove the home directory")
	}
	if filepath.Dir(cleaned) == cleaned {
		return fmt.Errorf("refusing to remove filesystem root %q", cleaned)
	}
	return nil
}
