package cmd

import (
	"strings"
)

// parseTaskFile parses a task file. One task per line.
// Blank lines and lines starting with # are ignored.
func parseTaskFile(content string) []string {
	var tasks []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tasks = append(tasks, line)
	}
	return tasks
}
