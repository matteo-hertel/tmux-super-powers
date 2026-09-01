package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Pane struct {
	ID    string
	Index int
}

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
		args = append(args, shellCommandArgs(command)...)
	}
	return args
}

func BuildSplitPaneArgs(target, dir, command string) []string {
	args := []string{
		"split-window", "-v", "-P", "-F", "#{pane_id}\t#{pane_index}",
		"-t", target, "-c", dir,
	}
	if command != "" {
		args = append(args, "/bin/sh", "-c", command)
	}
	return args
}

func SplitPane(session string, parentPane Pane, dir, command string) (Pane, error) {
	target := paneTarget(session, parentPane)
	out, err := exec.Command("tmux", BuildSplitPaneArgs(target, dir, command)...).CombinedOutput()
	if err != nil {
		return Pane{}, fmt.Errorf("failed to split pane: %w: %s", err, strings.TrimSpace(string(out)))
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 || parts[0] == "" {
		return Pane{}, fmt.Errorf("read split pane target: %q", strings.TrimSpace(string(out)))
	}
	paneIndex, err := strconv.Atoi(parts[1])
	if err != nil {
		return Pane{}, fmt.Errorf("read split pane index: %w", err)
	}
	return Pane{ID: parts[0], Index: paneIndex}, nil
}

func PaneExists(session string, pane Pane) bool {
	for _, candidate := range Panes(session) {
		if pane.ID != "" {
			if candidate.ID == pane.ID {
				return true
			}
			continue
		}
		if candidate.Index == pane.Index {
			return true
		}
	}
	return false
}

func Panes(session string) []Pane {
	out, err := exec.Command("tmux", "list-panes", "-t", session+":0", "-F", "#{pane_id}\t#{pane_index}").Output()
	if err != nil {
		return nil
	}
	var panes []Pane
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		index, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil {
			continue
		}
		panes = append(panes, Pane{ID: parts[0], Index: index})
	}
	return panes
}

func KillPane(session string, pane Pane) error {
	if !SessionExists(session) || !PaneExists(session, pane) {
		return nil
	}
	target := paneTarget(session, pane)
	if err := exec.Command("tmux", "kill-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("kill pane %s: %w", target, err)
	}
	return nil
}

func SelectPane(session string, pane Pane) error {
	target := paneTarget(session, pane)
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

// PreviewCaptureLines bounds the roster snapshot. The dashboard renders at most
// a terminal height of tail lines, so capturing the full scrollback costs tens of
// megabytes per pane to display a few dozen rows.
const PreviewCaptureLines = 400

// BuildPreviewCaptureArgs builds tmux capture-pane args for a bounded snapshot.
func BuildPreviewCaptureArgs(target string) []string {
	return []string{"capture-pane", "-t", target, "-p", "-e", "-S", "-" + strconv.Itoa(PreviewCaptureLines)}
}

// CreateTwoPaneSession creates a tmux session with a left and right pane.
// Uses -c flag for directory — no shell injection via send-keys.
func CreateTwoPaneSession(name, dir, leftCmd, rightCmd string) error {
	args := BuildNewSessionArgs(name, dir, leftCmd)
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	splitArgs := BuildTwoPaneSplitArgs(name, dir, rightCmd)
	if err := exec.Command("tmux", splitArgs...).Run(); err != nil {
		return fmt.Errorf("failed to split window: %w", err)
	}

	exec.Command("tmux", "select-pane", "-t", name+":0.0").Run()
	return nil
}

func BuildTwoPaneSplitArgs(name, dir, rightCmd string) []string {
	args := []string{"split-window", "-h", "-l", "20%", "-t", name, "-c", dir}
	if rightCmd != "" {
		args = append(args, shellCommandArgs(rightCmd)...)
	}
	return args
}

func shellCommandArgs(command string) []string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-c", command}
}

// GetPaneCwd returns the current working directory of a session's first pane.
func GetPaneCwd(session string) string {
	return GetPaneCwdFor(session, Pane{Index: 0})
}

// GetPaneCwdByIndex returns the current working directory of a specific pane.
func GetPaneCwdByIndex(session string, pane int) string {
	return GetPaneCwdFor(session, Pane{Index: pane})
}

func GetPaneCwdFor(session string, pane Pane) string {
	target := paneTarget(session, pane)
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func paneTarget(session string, pane Pane) string {
	if pane.ID != "" {
		return pane.ID
	}
	return fmt.Sprintf("%s:0.%d", session, pane.Index)
}
