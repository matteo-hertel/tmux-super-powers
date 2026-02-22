package pathutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExpandDirectories resolves directory patterns (plain paths, * globs, ** deep globs)
// into a list of actual directories. For ** patterns it walks up to 2 levels deep,
// only including git repos/worktrees. Ignores hidden dirs and gitignored paths.
func ExpandDirectories(patterns, ignoreDirectories []string) []string {
	var dirs []string
	ignoreSet := buildIgnoreSet(ignoreDirectories)

	for _, pattern := range patterns {
		expanded := ExpandPath(pattern)

		if strings.HasSuffix(expanded, "**") {
			basePath := strings.TrimSuffix(expanded, "**")
			basePath = strings.TrimSuffix(basePath, string(os.PathSeparator))
			walkDirectoryDepth(basePath, 2, ignoreSet, func(path string) {
				dirs = append(dirs, path)
			})
		} else if strings.Contains(expanded, "*") {
			matches, err := filepath.Glob(expanded)
			if err != nil {
				continue
			}
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || !info.IsDir() {
					continue
				}
				if shouldIgnoreDir(filepath.Base(match), ignoreSet) {
					continue
				}
				if isGitIgnored(match) {
					continue
				}
				dirs = append(dirs, match)
			}
		} else {
			if info, err := os.Stat(expanded); err == nil && info.IsDir() {
				dirs = append(dirs, expanded)
			}
		}
	}
	return dirs
}

// DedupeStrings removes duplicates from a string slice, preserving order.
func DedupeStrings(items []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}
	return unique
}

func buildIgnoreSet(userIgnores []string) map[string]bool {
	set := make(map[string]bool)
	for _, name := range userIgnores {
		set[strings.ToLower(name)] = true
	}
	return set
}

func shouldIgnoreDir(name string, userIgnores map[string]bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if userIgnores[strings.ToLower(name)] {
		return true
	}
	return false
}

func isGitRoot(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func isGitIgnored(path string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", path)
	cmd.Dir = filepath.Dir(path)
	return cmd.Run() == nil
}

func gitIgnoredSet(dir string, paths []string) map[string]bool {
	ignored := make(map[string]bool)
	if len(paths) == 0 {
		return ignored
	}
	cmd := exec.Command("git", "check-ignore", "--stdin")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n"))
	out, err := cmd.Output()
	if err != nil {
		return ignored
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			ignored[line] = true
		}
	}
	return ignored
}

func walkDirectoryDepth(root string, maxDepth int, ignoreSet map[string]bool, fn func(string)) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	fn(root)
	walkDirectoryDepthRecursive(root, 0, maxDepth, ignoreSet, fn)
}

func walkDirectoryDepthRecursive(dir string, currentDepth, maxDepth int, ignoreSet map[string]bool, fn func(string)) {
	if currentDepth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() || shouldIgnoreDir(entry.Name(), ignoreSet) {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, entry.Name()))
	}

	ignored := gitIgnoredSet(dir, candidates)
	for _, path := range candidates {
		if ignored[path] {
			continue
		}
		if isGitRoot(path) {
			fn(path)
		} else {
			walkDirectoryDepthRecursive(path, currentDepth+1, maxDepth, ignoreSet, fn)
		}
	}
}
