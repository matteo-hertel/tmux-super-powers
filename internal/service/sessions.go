package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]|\x1b[=>]`)

// StripANSI removes terminal control sequences from captured pane output.
func StripANSI(text string) string {
	return strings.ReplaceAll(ansiEscapePattern.ReplaceAllString(text, ""), "\r", "")
}

// PaneTypeFromProcess classifies a pane's process into a category.
// Returns "editor", "agent", "shell", or "process".
func PaneTypeFromProcess(process string) string {
	switch process {
	case "nvim", "vim", "emacs", "nano":
		return "editor"
	case "claude", "aider", "codex":
		return "agent"
	case "bash", "zsh", "fish", "sh", "":
		return "shell"
	default:
		// Claude Code reports its version (e.g. "2.1.71") as the process name.
		if isClaudeVersion(process) || DetectAgentProvider(process) != AgentProviderFallback {
			return "agent"
		}
		return "process"
	}
}

// isClaudeVersion returns true if the process name looks like a semver version
// (e.g. "2.1.71"), which is how Claude Code appears in tmux pane_current_command.
func isClaudeVersion(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// ListSessions returns all tmux session names.
// Returns an empty slice (not an error) if tmux server is not running.
func ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		// "no server running" is not an error — just means zero sessions.
		if strings.Contains(err.Error(), "exit status") {
			errOut := ""
			if ee, ok := err.(*exec.ExitError); ok {
				errOut = string(ee.Stderr)
			}
			if strings.Contains(errOut, "no server running") || strings.Contains(errOut, "no current") {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("list-sessions: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// GetAgentPaneCwd returns the working directory of a specific pane.
func GetAgentPaneCwd(session string, pane int) string {
	return tmuxpkg.GetPaneCwdByIndex(session, pane)
}

// AgentProcessInfo describes the controllable agent process for a pane.
type AgentProcessInfo struct {
	Provider string
	PID      int
	Command  string
}

// DetectPaneAgentProcess resolves the agent process for a tmux pane. It checks
// the pane process first, then direct shell children for wrapped agent commands.
func DetectPaneAgentProcess(session string, pane int, paneProcess string) AgentProcessInfo {
	panePID := GetPanePID(session, pane)
	provider := DetectAgentProvider(paneProcess)
	if provider != AgentProviderFallback && panePID != 0 {
		return AgentProcessInfo{Provider: provider, PID: panePID, Command: paneProcess}
	}
	if panePID == 0 {
		return AgentProcessInfo{Provider: provider, Command: paneProcess}
	}
	if child := findAgentProcessInfo(fmt.Sprintf("%d", panePID)); child.Provider != "" {
		return child
	}
	if paneProcess == "aider" {
		return AgentProcessInfo{Provider: AgentProviderFallback, PID: panePID, Command: paneProcess}
	}
	return AgentProcessInfo{Provider: provider, PID: panePID, Command: paneProcess}
}

// GetPanePID returns the root process ID for a tmux pane.
func GetPanePID(session string, pane int) int {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	pidCmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_pid}")
	pidOut, err := pidCmd.Output()
	if err != nil {
		return 0
	}
	panePid := strings.TrimSpace(string(pidOut))
	if panePid == "" {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(panePid, "%d", &pid); err != nil {
		return 0
	}
	return pid
}

func findAgentProcessInfo(shellPid string) AgentProcessInfo {
	pgrepCmd := exec.Command("pgrep", "-P", shellPid)
	pgrepOut, err := pgrepCmd.Output()
	if err != nil {
		return AgentProcessInfo{}
	}
	for _, pid := range strings.Split(strings.TrimSpace(string(pgrepOut)), "\n") {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			continue
		}
		commCmd := exec.Command("ps", "-p", pid, "-o", "comm=")
		commOut, err := commCmd.Output()
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(string(commOut))
		provider := DetectAgentProvider(comm)
		if provider != AgentProviderFallback || comm == "aider" {
			var pidInt int
			_, _ = fmt.Sscanf(pid, "%d", &pidInt)
			return AgentProcessInfo{Provider: provider, PID: pidInt, Command: comm}
		}
	}
	return AgentProcessInfo{}
}

// hasAgentChild checks if a shell pane has an agent (claude/aider/codex) as a child process.
func hasAgentChild(session string, pane int) bool {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	pidCmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_pid}")
	pidOut, err := pidCmd.Output()
	if err != nil {
		return false
	}
	panePid := strings.TrimSpace(string(pidOut))
	if panePid == "" {
		return false
	}
	return findAgentProcessInfo(panePid).Provider != ""
}

// GetPaneProcess returns the current command running in a specific pane.
func GetPaneProcess(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_current_command}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetPaneCount returns the number of panes in a session's first window.
func GetPaneCount(session string) int {
	cmd := exec.Command("tmux", "list-panes", "-t", session, "-F", "#{pane_index}")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return 0
	}
	return len(strings.Split(raw, "\n"))
}

// CapturePaneContent captures the visible content of a pane.
// Falls back to pane 0 if the requested pane fails.
func CapturePaneContent(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	args := tmuxpkg.BuildCapturePaneArgs(target)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	if err != nil {
		// Fall back to pane 0 if the requested pane failed.
		if pane != 0 {
			fallbackTarget := fmt.Sprintf("%s:0.0", session)
			fallbackArgs := tmuxpkg.BuildCapturePaneArgs(fallbackTarget)
			fallbackCmd := exec.Command("tmux", fallbackArgs...)
			fallbackOut, fallbackErr := fallbackCmd.Output()
			if fallbackErr != nil {
				return ""
			}
			return StripANSI(string(fallbackOut))
		}
		return ""
	}
	return StripANSI(string(out))
}

// GitInfo holds git repository metadata for a session.
type GitInfo struct {
	Cwd          string // pane working directory
	GitPath      string // toplevel of the main repo
	Branch       string
	IsWorktree   bool
	WorktreePath string // the worktree directory (only set when IsWorktree is true)
}

// DetectSessionGitInfo checks if a session's working directory is inside a git repo.
// Returns the git toplevel path and current branch name, or empty strings if not a git repo.
func DetectSessionGitInfo(sessionName string) (gitPath, branch string) {
	info := DetectSessionGitInfoFull(sessionName)
	return info.GitPath, info.Branch
}

// DetectSessionGitInfoFull checks if a session's working directory is inside a git repo
// and detects whether it is a git worktree.
func DetectSessionGitInfoFull(sessionName string) GitInfo {
	cwd := tmuxpkg.GetPaneCwd(sessionName)
	if cwd == "" {
		return GitInfo{}
	}
	// Check if it's a git repo and get the toplevel
	topCmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	topOut, err := topCmd.Output()
	if err != nil {
		return GitInfo{Cwd: cwd}
	}
	gitPath := strings.TrimSpace(string(topOut))

	// Get current branch
	var branch string
	branchCmd := exec.Command("git", "-C", gitPath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.Output()
	if err == nil {
		branch = strings.TrimSpace(string(branchOut))
	}

	info := GitInfo{GitPath: gitPath, Branch: branch}
	info.Cwd = cwd

	// Detect worktree: compare --git-dir vs --git-common-dir.
	// In a worktree they differ; in the main checkout they are the same.
	gitDirCmd := exec.Command("git", "-C", cwd, "rev-parse", "--git-dir")
	commonDirCmd := exec.Command("git", "-C", cwd, "rev-parse", "--git-common-dir")
	gitDirOut, err1 := gitDirCmd.Output()
	commonDirOut, err2 := commonDirCmd.Output()
	if err1 == nil && err2 == nil {
		gitDir := strings.TrimSpace(string(gitDirOut))
		commonDir := strings.TrimSpace(string(commonDirOut))
		// Resolve to absolute paths for reliable comparison
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(cwd, gitDir)
		}
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(cwd, commonDir)
		}
		gitDir = filepath.Clean(gitDir)
		commonDir = filepath.Clean(commonDir)
		if gitDir != commonDir {
			info.IsWorktree = true
			info.WorktreePath = gitPath // toplevel of the worktree
			// The main repo is the parent of the common dir (.git)
			info.GitPath = filepath.Dir(commonDir)
		}
	}

	return info
}

// KillSession kills a tmux session by name and optionally cleans up an associated git worktree.
// gitPath is the main repo path used with -C so git commands run in the correct repo.
func KillSession(name string, cleanupWorktree bool, worktreePath, branch, gitPath string) error {
	if tmuxpkg.SessionExists(name) {
		if err := tmuxpkg.KillSession(name); err != nil {
			return fmt.Errorf("kill session %q: %w", name, err)
		}
	}

	if cleanupWorktree && worktreePath != "" {
		repoFlag := gitPath
		if repoFlag == "" {
			repoFlag = worktreePath // fallback if no main repo path
		}
		// Try git worktree remove first
		rmCmd := exec.Command("git", "-C", repoFlag, "worktree", "remove", worktreePath, "--force")
		if err := rmCmd.Run(); err != nil {
			// Fallback: remove directory manually then prune
			os.RemoveAll(worktreePath)
			pruneCmd := exec.Command("git", "-C", repoFlag, "worktree", "prune")
			_ = pruneCmd.Run()
		}
		// Delete the branch if provided
		if branch != "" {
			branchCmd := exec.Command("git", "-C", repoFlag, "branch", "-D", branch)
			_ = branchCmd.Run() // best-effort: branch may already be gone
		}
		// Clean up empty parent directories
		CleanupEmptyParents(filepath.Dir(worktreePath))
	}
	return nil
}

// CleanupEmptyParents walks up from dir removing empty directories.
// Stops at the user's home directory to avoid removing too much.
func CleanupEmptyParents(dir string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	for {
		dir = filepath.Clean(dir)
		// Stop at home dir or root
		if dir == homeDir || dir == "/" || dir == "." {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return // not empty or can't read — stop
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// CreateSession creates a new tmux session with a two-pane layout.
// Returns an error if the session already exists.
func CreateSession(name, dir, leftCmd, rightCmd string) error {
	if tmuxpkg.SessionExists(name) {
		return fmt.Errorf("session %q already exists", name)
	}
	return tmuxpkg.CreateTwoPaneSession(name, dir, leftCmd, rightCmd)
}

// SendToPane sends text (followed by Enter) to a specific pane in a session.
func SendToPane(session string, pane int, text string) error {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	return tmuxpkg.SendKeys(target, text)
}

// InterruptPane sends Ctrl-C to a running agent without deleting its session or
// workspace.
func InterruptPane(session string, pane int) error {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", "send-keys", "-t", target, "C-c")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("interrupt agent pane: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
