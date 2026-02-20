package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type sessionInfo struct {
	name        string
	status      string // active, idle, done, error
	lastChanged time.Time
	prevContent string
	paneContent string
	paneCount   int
	currentPane int
}

// inferStatus determines session status from pane content changes.
func inferStatus(prev, current string, lastChanged, now time.Time, errorPatterns []string, promptPattern string) string {
	// Check for error patterns first (highest priority)
	for _, pattern := range errorPatterns {
		if strings.Contains(current, pattern) {
			return "error"
		}
	}

	// Content changed → active
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

	// Unchanged for >30s → idle
	if elapsed > 30*time.Second {
		return "idle"
	}

	return "active"
}

func statusIcon(status string) string {
	switch status {
	case "active":
		return "●"
	case "idle":
		return "◌"
	case "done":
		return "✓"
	case "error":
		return "✗"
	default:
		return "?"
	}
}

func formatTimeSince(since, now time.Time) string {
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
