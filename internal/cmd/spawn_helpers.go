package cmd

import (
	"regexp"
	"strings"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9-]+`)
var multiHyphen = regexp.MustCompile(`-{2,}`)

// taskToBranch converts a task description to a git branch name.
// "fix the auth bug" → "spawn/fix-the-auth-bug"
func taskToBranch(task string) string {
	if task == "" {
		return "spawn/task"
	}
	name := strings.ToLower(task)
	name = nonAlphaNumeric.ReplaceAllString(name, "-")
	name = multiHyphen.ReplaceAllString(name, "-")
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
