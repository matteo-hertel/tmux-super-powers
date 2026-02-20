package tmux

import (
	"fmt"
	"os"
	"os/exec"
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
