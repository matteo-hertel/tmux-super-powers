package service

import (
	"fmt"
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
	Task    string `json:"task"`
	Branch  string `json:"branch"`
	Session string `json:"session"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// SpawnAgents deploys agents with tasks into worktrees.
// If repoDir is non-empty, it is used to find the git repo root; otherwise the server's cwd is used.
func SpawnAgents(tasks []string, baseBranch string, noInstall bool, cfg *config.Config, repoDir string) ([]SpawnResult, error) {
	var repoRoot string
	var err error
	if repoDir != "" {
		repoRoot, err = spawnGetRepoRootFrom(repoDir)
	} else {
		repoRoot, err = spawnGetRepoRoot()
	}
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
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

		result := SpawnResult{Task: task, Branch: branch, Session: sessionName}

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

		if !noInstall {
			pm := spawnDetectPM(repoRoot)
			if pm != "" {
				spawnRunPM(pm, worktreePath, repoRoot)
			}
		}

		if tmuxpkg.SessionExists(sessionName) {
			tmuxpkg.KillSession(sessionName)
		}
		tmuxpkg.CreateTwoPaneSession(sessionName, worktreePath, "nvim", agentCmd)

		// Wait for the agent command to start before sending the task prompt
		time.Sleep(2 * time.Second)

		target := fmt.Sprintf("%s:0.1", sessionName)
		if err := tmuxpkg.SendKeys(target, task); err != nil {
			result.Status = "error"
			result.Error = fmt.Sprintf("failed to send task to pane: %v", err)
			results = append(results, result)
			continue
		}

		result.Status = "ok"
		results = append(results, result)
	}

	return results, nil
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

func spawnRunPM(pm, path, repoRoot string) {
	if pm == "yarn" {
		yarnDir := filepath.Join(path, ".yarn")
		os.MkdirAll(yarnDir, 0755)
		for _, name := range []string{"cache", "install-state.gz", "unplugged"} {
			src := filepath.Join(repoRoot, ".yarn", name)
			dst := filepath.Join(yarnDir, name)
			if _, err := os.Stat(src); err == nil {
				if _, err := os.Stat(dst); err != nil {
					exec.Command("cp", "-a", src, dst).Run()
				}
			}
		}
	}
	var cmd *exec.Cmd
	switch pm {
	case "yarn":
		cmd = exec.Command("yarn", "install")
	case "pnpm":
		cmd = exec.Command("pnpm", "install")
	case "bun":
		cmd = exec.Command("bun", "install")
	default:
		cmd = exec.Command("npm", "install")
	}
	cmd.Dir = path
	cmd.Run()
}
