package service

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

// Session represents a tmux session with enriched metadata.
type Session struct {
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Branch       string    `json:"branch,omitempty"`
	IsWorktree   bool      `json:"isWorktree"`
	IsGitRepo    bool      `json:"isGitRepo"`
	GitPath      string    `json:"-"`
	LastChanged  time.Time `json:"lastChanged"`
	Panes        []Pane    `json:"panes"`
	Diff         *DiffStat `json:"diff,omitempty"`
	PR           *PRInfo   `json:"pr,omitempty"`
	PrevContent  string    `json:"-"`
	WorktreePath string    `json:"-"`
}

// Pane represents a single pane within a tmux session.
type Pane struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`              // editor, agent, shell, process
	Process string `json:"process"`
	Status  string `json:"status,omitempty"`
	Content string `json:"content,omitempty"`
}

// DiffStat holds git diff statistics.
type DiffStat struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

// PRInfo holds pull request metadata.
type PRInfo struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	CIStatus    string `json:"ciStatus"`
	ReviewCount int    `json:"reviewCount"`
}

// PaneTypeFromProcess classifies a pane's process into a category.
// Returns "editor", "agent", "shell", or "process".
func PaneTypeFromProcess(process string) string {
	switch process {
	case "nvim", "vim", "emacs", "nano":
		return "editor"
	case "claude":
		return "agent"
	case "bash", "zsh", "fish", "sh", "":
		return "shell"
	default:
		return "process"
	}
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
			return string(fallbackOut)
		}
		return ""
	}
	return string(out)
}

// DetectSessionGitInfo checks if a session's working directory is inside a git repo.
// Returns the git toplevel path and current branch name, or empty strings if not a git repo.
func DetectSessionGitInfo(sessionName string) (gitPath, branch string) {
	cwd := tmuxpkg.GetPaneCwd(sessionName)
	if cwd == "" {
		return "", ""
	}
	// Check if it's a git repo and get the toplevel
	topCmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	topOut, err := topCmd.Output()
	if err != nil {
		return "", ""
	}
	gitPath = strings.TrimSpace(string(topOut))

	// Get current branch
	branchCmd := exec.Command("git", "-C", gitPath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.Output()
	if err != nil {
		return gitPath, ""
	}
	branch = strings.TrimSpace(string(branchOut))
	return gitPath, branch
}

// KillSession kills a tmux session by name and optionally cleans up an associated git worktree.
func KillSession(name string, cleanupWorktree bool, worktreePath, branch string) error {
	if err := tmuxpkg.KillSession(name); err != nil {
		return fmt.Errorf("kill session %q: %w", name, err)
	}

	if cleanupWorktree && worktreePath != "" {
		// Remove the git worktree
		rmCmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
		if err := rmCmd.Run(); err != nil {
			return fmt.Errorf("remove worktree %q: %w", worktreePath, err)
		}
		// Delete the branch if provided
		if branch != "" {
			branchCmd := exec.Command("git", "branch", "-D", branch)
			_ = branchCmd.Run() // best-effort: branch may already be gone
		}
	}
	return nil
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

// TmuxRunning returns true if the tmux server is running (has at least one session).
func TmuxRunning() bool {
	cmd := exec.Command("tmux", "list-sessions")
	return cmd.Run() == nil
}

// GhAvailable returns true if the GitHub CLI (gh) is on $PATH.
func GhAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// sessionsWrapper is used for JSON marshalling with a top-level key.
type sessionsWrapper struct {
	Sessions []Session `json:"sessions"`
}

// MarshalSessions serializes sessions to JSON with a {"sessions": [...]} envelope.
func MarshalSessions(sessions []Session) ([]byte, error) {
	return json.Marshal(sessionsWrapper{Sessions: sessions})
}
