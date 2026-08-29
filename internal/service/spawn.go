package service

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9-]+`)
var multiDash = regexp.MustCompile(`-{2,}`)

// Memorable word pairs for unique session/branch suffixes.
var adjectives = []string{
	"red", "blue", "bold", "calm", "cold", "cool", "dark", "deep",
	"dry", "fast", "gold", "gray", "keen", "loud", "mint", "pale",
	"pink", "pure", "soft", "warm", "wide", "wild", "zen", "neon",
}
var nouns = []string{
	"arch", "beam", "bolt", "cape", "claw", "coil", "dawn", "edge",
	"fern", "flux", "glow", "haze", "iris", "jade", "knot", "lark",
	"mars", "node", "oak", "peak", "reef", "sage", "tide", "volt",
}

// memorableSuffix returns a short, memorable two-word suffix like "bold-tide".
func memorableSuffix() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return adjectives[r.Intn(len(adjectives))] + "-" + nouns[r.Intn(len(nouns))]
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// TaskToBranch converts a task description to a git branch name.
func TaskToBranch(task string) string {
	if task == "" {
		return "spawn/task"
	}
	name := strings.ToLower(task)
	name = nonAlphaNum.ReplaceAllString(name, "-")
	name = multiDash.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 50 {
		name = name[:50]
		if idx := strings.LastIndex(name, "-"); idx > 0 {
			name = name[:idx]
		}
		name = strings.TrimRight(name, "-")
	}
	return "spawn/" + name
}

// SpawnResult holds the result of spawning a single agent.
type SpawnResult struct {
	Task         string `json:"task"`
	Branch       string `json:"branch"`
	Session      string `json:"session"`
	PaneIndex    int    `json:"paneIndex"`
	Command      string `json:"command,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
	GitPath      string `json:"gitPath,omitempty"`
	AgentRunID   string `json:"agentRunId,omitempty"`
}

// SpawnDelegatedAgent starts a short-lived agent in an existing workspace. It
// deliberately creates no branch or worktree: the caller records the new run
// as a child of the run that owns the workspace.
func SpawnDelegatedAgent(task, prompt, dir, session string, parentPane int, branch, gitPath, agentCommand string) (SpawnResult, error) {
	dir = pathutil.ExpandPath(strings.TrimSpace(dir))
	result := SpawnResult{
		Task:         task,
		Branch:       branch,
		Session:      session,
		WorktreePath: dir,
		GitPath:      gitPath,
	}
	if dir == "" {
		return result, fmt.Errorf("delegation workspace is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return result, fmt.Errorf("delegation workspace: %w", err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("delegation workspace is not a directory: %s", dir)
	}
	agentCommand = strings.TrimSpace(agentCommand)
	if agentCommand == "" {
		return result, fmt.Errorf("manager agent command is required")
	}
	if strings.TrimSpace(session) == "" {
		return result, fmt.Errorf("parent tmux session is required")
	}
	if parentPane < 0 {
		return result, fmt.Errorf("parent tmux pane is required")
	}
	if !tmuxpkg.SessionExists(session) {
		return result, fmt.Errorf("parent tmux session %q does not exist", session)
	}
	targetPane := parentPane
	if !tmuxpkg.PaneExists(session, targetPane) {
		paneIndices := tmuxpkg.PaneIndices(session)
		if len(paneIndices) == 0 {
			return result, fmt.Errorf("parent tmux session %q has no panes", session)
		}
		targetPane = paneIndices[0]
	}

	result.Command = BuildAgentCommand(agentCommand, prompt)
	paneIndex, err := tmuxpkg.SplitPane(session, targetPane, dir, result.Command)
	if err != nil {
		return result, fmt.Errorf("delegated pane creation failed: %w", err)
	}
	result.PaneIndex = paneIndex
	if err := tmuxpkg.KeepPaneAfterExit(fmt.Sprintf("%s:0.%d", session, paneIndex)); err != nil {
		_ = tmuxpkg.KillPane(session, paneIndex)
		return result, err
	}
	result.Status = "ok"
	return result, nil
}

// SpawnAgents deploys agents with tasks into worktrees (git repos) or
// directly in the target directory (non-git directories).
// If repoDir is non-empty, it is used to find the git repo root; otherwise the
// current working directory is used.
func SpawnAgents(tasks []string, baseBranch string, noInstall bool, cfg *config.Config, repoDir string) ([]SpawnResult, error) {
	var repoRoot string
	var err error
	if repoDir != "" {
		repoRoot, err = spawnGetRepoRootFrom(repoDir)
	} else {
		repoRoot, err = spawnGetRepoRoot()
	}

	// Non-git directory: spawn agents directly without worktrees.
	if err != nil {
		dir := repoDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		dir = pathutil.ExpandPath(dir)
		return spawnDirect(tasks, dir, cfg)
	}

	repoName := filepath.Base(repoRoot)

	if baseBranch == "" {
		baseBranch, err = spawnGetCurrentBranch(repoRoot)
		if err != nil {
			return nil, fmt.Errorf("cannot determine current branch: %w", err)
		}
	}

	worktreeBase := pathutil.ExpandPath(cfg.Spawn.WorktreeBase)
	agentCmd := cfg.Spawn.AgentCommand

	var results []SpawnResult
	for _, task := range tasks {
		suffix := memorableSuffix()
		branch := TaskToBranch(task) + "-" + suffix
		branchShort := strings.TrimPrefix(branch, "spawn/")
		sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s", repoName, branchShort))
		worktreePath := filepath.Join(worktreeBase, fmt.Sprintf("%s-%s", repoName, branchShort))

		result := SpawnResult{
			Task:         task,
			Branch:       branch,
			Session:      sessionName,
			PaneIndex:    1,
			Command:      BuildAgentCommand(agentCmd, task),
			WorktreePath: worktreePath,
			GitPath:      repoRoot,
		}

		if !spawnBranchExists(repoRoot, branch) {
			if err := spawnCreateBranch(repoRoot, branch, baseBranch); err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("branch creation failed: %v", err)
				results = append(results, result)
				continue
			}
		}

		if _, err := os.Stat(worktreePath); err != nil {
			if err := spawnCreateWorktree(repoRoot, worktreePath, branch); err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("worktree creation failed: %v", err)
				results = append(results, result)
				continue
			}
		}

		if cfg.Spawn.DefaultSetup != "" {
			if err := spawnRunSetup(cfg.Spawn.DefaultSetup, worktreePath); err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("setup failed: %v", err)
				results = append(results, result)
				continue
			}
		}

		if tmuxpkg.SessionExists(sessionName) {
			tmuxpkg.KillSession(sessionName)
		}
		// Pass the task as a CLI argument to the agent command so it starts
		// working immediately — avoids all send-keys/Enter issues.
		if err := tmuxpkg.CreateTwoPaneSession(sessionName, worktreePath, "nvim", result.Command); err != nil {
			result.Status = "error"
			result.Error = fmt.Sprintf("session creation failed: %v", err)
			results = append(results, result)
			continue
		}

		result.Status = "ok"
		results = append(results, result)
	}

	return results, nil
}

// spawnDirect creates agents directly in a directory without git worktrees.
// Each task gets its own tmux session running the agent command in the target dir.
func spawnDirect(tasks []string, dir string, cfg *config.Config) ([]SpawnResult, error) {
	dirName := filepath.Base(dir)
	agentCmd := cfg.Spawn.AgentCommand

	var results []SpawnResult
	for _, task := range tasks {
		suffix := memorableSuffix()
		slug := TaskToBranch(task)
		slug = strings.TrimPrefix(slug, "spawn/")
		sessionName := tmuxpkg.SanitizeSessionName(fmt.Sprintf("%s-%s-%s", dirName, slug, suffix))

		result := SpawnResult{
			Task:      task,
			Session:   sessionName,
			PaneIndex: 1,
			Command:   BuildAgentCommand(agentCmd, task),
			Status:    "ok",
		}

		if tmuxpkg.SessionExists(sessionName) {
			tmuxpkg.KillSession(sessionName)
		}

		if cfg.Spawn.DefaultSetup != "" {
			if err := spawnRunSetup(cfg.Spawn.DefaultSetup, dir); err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("setup failed: %v", err)
				results = append(results, result)
				continue
			}
		}

		if err := tmuxpkg.CreateTwoPaneSession(sessionName, dir, "nvim", result.Command); err != nil {
			result.Status = "error"
			result.Error = fmt.Sprintf("session creation failed: %v", err)
		}

		results = append(results, result)
	}

	return results, nil
}

func BuildAgentCommand(command, prompt string) string {
	return strings.TrimSpace(command) + " " + shellQuote(prompt)
}

func spawnRunSetup(command, dir string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	return cmd.Run()
}

func spawnGetRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func spawnGetRepoRootFrom(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func spawnGetCurrentBranch(repoRoot string) (string, error) {
	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func spawnBranchExists(repoRoot, branch string) bool {
	return exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", fmt.Sprintf("refs/heads/%s", branch)).Run() == nil
}

func spawnCreateBranch(repoRoot, branch, from string) error {
	return exec.Command("git", "-C", repoRoot, "branch", branch, from).Run()
}

func spawnCreateWorktree(repoRoot, path, branch string) error {
	return exec.Command("git", "-C", repoRoot, "worktree", "add", path, branch).Run()
}

func spawnDetectPM(repoRoot string) string {
	for _, lf := range []struct{ file, pm string }{
		{"bun.lockb", "bun"}, {"bun.lock", "bun"},
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	} {
		if _, err := os.Stat(filepath.Join(repoRoot, lf.file)); err == nil {
			return lf.pm
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "package.json")); err == nil {
		return "npm"
	}
	return ""
}

// spawnCopyNodeModules hardlink-copies node_modules from repoRoot to worktreePath.
// Uses filepath.WalkDir + os.Link for platform-agnostic hardlinks (works on macOS and Linux).
// Silently returns nil if node_modules doesn't exist in repoRoot.
func spawnCopyNodeModules(repoRoot, worktreePath string) error {
	srcNM := filepath.Join(repoRoot, "node_modules")
	if _, err := os.Stat(srcNM); err != nil {
		return nil
	}
	dstNM := filepath.Join(worktreePath, "node_modules")
	return filepath.WalkDir(srcNM, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, _ := filepath.Rel(srcNM, path)
		dst := filepath.Join(dstNM, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return nil
			}
			return os.Symlink(target, dst)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return os.Link(path, dst)
	})
}
