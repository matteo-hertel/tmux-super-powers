package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

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
