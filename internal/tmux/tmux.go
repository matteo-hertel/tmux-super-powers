package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SanitizeSessionName replaces tmux-problematic characters (. and :) with hyphens.
func SanitizeSessionName(name string) string {
	r := strings.NewReplacer(".", "-", ":", "-")
	return r.Replace(name)
}

// IsInsideTmux returns true if running inside a tmux session.
func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// KillSession kills a tmux session by name.
func KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

// AttachOrSwitch attaches to or switches to a tmux session.
// Uses switch-client when inside tmux, attach-session when outside.
func AttachOrSwitch(name string) error {
	if IsInsideTmux() {
		return exec.Command("tmux", "switch-client", "-t", name).Run()
	}
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildNewSessionArgs builds the tmux args for creating a new session.
// Uses -c flag for working directory (no shell injection).
func BuildNewSessionArgs(name, dir, command string) []string {
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	if command != "" {
		args = append(args, command)
	}
	return args
}

func BuildSplitPaneArgs(target, dir, command string) []string {
	args := []string{
		"split-window", "-v", "-P", "-F", "#{pane_index}",
		"-t", target, "-c", dir,
	}
	if command != "" {
		args = append(args, command)
	}
	return args
}

func SplitPane(session string, parentPane int, dir, command string) (int, error) {
	target := fmt.Sprintf("%s:0.%d", session, parentPane)
	out, err := exec.Command("tmux", BuildSplitPaneArgs(target, dir, command)...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("failed to split pane: %w: %s", err, strings.TrimSpace(string(out)))
	}
	paneIndex, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("read split pane index: %w", err)
	}
	return paneIndex, nil
}

func PaneExists(session string, pane int) bool {
	for _, paneIndex := range PaneIndices(session) {
		if paneIndex == pane {
			return true
		}
	}
	return false
}

func PaneIndices(session string) []int {
	out, err := exec.Command("tmux", "list-panes", "-t", session+":0", "-F", "#{pane_index}").Output()
	if err != nil {
		return nil
	}
	var indices []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		index, parseErr := strconv.Atoi(strings.TrimSpace(line))
		if parseErr == nil {
			indices = append(indices, index)
		}
	}
	return indices
}

func KillPane(session string, pane int) error {
	if !SessionExists(session) || !PaneExists(session, pane) {
		return nil
	}
	target := fmt.Sprintf("%s:0.%d", session, pane)
	if err := exec.Command("tmux", "kill-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("kill pane %s: %w", target, err)
	}
	return nil
}

func SelectPane(session string, pane int) error {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	if err := exec.Command("tmux", "select-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("select pane %s: %w", target, err)
	}
	return nil
}

// BuildPopupArgs builds the tmux args for display-popup.
func BuildPopupArgs(command string, width, height int) []string {
	return []string{
		"display-popup", "-E",
		"-w", fmt.Sprintf("%d%%", width),
		"-h", fmt.Sprintf("%d%%", height),
		command,
	}
}

// RunPopup runs a command in a tmux display-popup overlay.
// If detach is true, the popup is launched in the background and control returns immediately.
func RunPopup(command string, width, height int, detach bool) error {
	args := BuildPopupArgs(command, width, height)
	cmd := exec.Command("tmux", args...)
	if detach {
		return cmd.Start()
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildCapturePaneArgs builds tmux capture-pane args.
func BuildCapturePaneArgs(target string) []string {
	return []string{"capture-pane", "-t", target, "-p", "-e"}
}

// CreateTwoPaneSession creates a tmux session with a left and right pane.
// Uses -c flag for directory — no shell injection via send-keys.
func CreateTwoPaneSession(name, dir, leftCmd, rightCmd string) error {
	args := BuildNewSessionArgs(name, dir, leftCmd)
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	splitArgs := []string{"split-window", "-h", "-t", name, "-c", dir}
	if rightCmd != "" {
		splitArgs = append(splitArgs, rightCmd)
	}
	if err := exec.Command("tmux", splitArgs...).Run(); err != nil {
		return fmt.Errorf("failed to split window: %w", err)
	}

	exec.Command("tmux", "select-pane", "-t", name+":0.0").Run()
	return nil
}

// KeepPaneAfterExit retains a pane and its scrollback after its command exits.
// This is used for one-shot delegated agents so their final result remains
// inspectable in the dashboard.
func KeepPaneAfterExit(target string) error {
	if err := exec.Command("tmux", BuildKeepPaneAfterExitArgs(target)...).Run(); err != nil {
		return fmt.Errorf("retain pane after exit: %w", err)
	}
	return nil
}

func BuildKeepPaneAfterExitArgs(target string) []string {
	return []string{"set-option", "-p", "-t", target, "remain-on-exit", "on"}
}

// GetPaneCwd returns the current working directory of a session's first pane.
func GetPaneCwd(session string) string {
	return GetPaneCwdByIndex(session, 0)
}

// GetPaneCwdByIndex returns the current working directory of a specific pane.
func GetPaneCwdByIndex(session string, pane int) string {
	target := fmt.Sprintf("%s:0.%d", session, pane)
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
