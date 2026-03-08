package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ansiRe matches ANSI escape sequences (CSI, OSC, and single-char escapes).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]|\x1b[=>]`)

// InferStatus determines session status from pane content changes.
// Priority: error > active (content changed) > done (>60s, prompt visible) > idle (>30s) > active
func InferStatus(prev, current string, lastChanged, now time.Time, errorPatterns []string, promptPattern string) string {
	// Check for error patterns first (highest priority)
	for _, pattern := range errorPatterns {
		if strings.Contains(current, pattern) {
			return "error"
		}
	}
	// Content changed -> active
	if prev != current {
		return "active"
	}
	elapsed := now.Sub(lastChanged)
	// Check for shell prompt (done state)
	if elapsed > 60*time.Second && promptPattern != "" {
		if re, err := regexp.Compile(promptPattern); err == nil {
			lines := strings.Split(strings.TrimRight(current, "\n"), "\n")
			if len(lines) > 0 {
				lastLine := strings.TrimRight(lines[len(lines)-1], " ")
				if re.MatchString(lastLine) {
					return "done"
				}
			}
		}
	}
	// Unchanged for >30s -> idle
	if elapsed > 30*time.Second {
		return "idle"
	}
	return "active"
}

// WaitingPane records which pane is waiting and its prompt text.
type WaitingPane struct {
	Index  int
	Prompt string
}

// DetectWaitingPanes checks each agent pane individually for input prompt patterns.
func DetectWaitingPanes(panes []Pane, inputPatterns []string) []WaitingPane {
	var result []WaitingPane
	for _, pane := range panes {
		if pane.Type != "agent" || pane.Content == "" {
			continue
		}
		lines := strings.Split(strings.TrimRight(pane.Content, "\n"), "\n")
		check := lines
		if len(check) > 5 {
			check = check[len(check)-5:]
		}
		matched := false
		for _, pattern := range inputPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			for _, line := range check {
				cleaned := ansiRe.ReplaceAllString(line, "")
				if re.MatchString(cleaned) {
					prompt := lines
					if len(prompt) > 3 {
						prompt = prompt[len(prompt)-3:]
					}
					result = append(result, WaitingPane{
						Index:  pane.Index,
						Prompt: strings.Join(prompt, "\n"),
					})
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}
	return result
}

// StatusIcon returns a Unicode icon for a status string.
func StatusIcon(status string) string {
	switch status {
	case "active":
		return "\u25cf"
	case "idle":
		return "\u25cc"
	case "done":
		return "\u2713"
	case "error":
		return "\u2717"
	case "waiting":
		return "?"
	default:
		return "?"
	}
}

// FormatTimeSince formats a duration since a time as a human-readable string.
func FormatTimeSince(since, now time.Time) string {
	d := now.Sub(since)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
