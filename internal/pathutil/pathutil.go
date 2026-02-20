package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands ~ prefix to the user's home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// ExpandEnvVar expands a $VAR string to its environment variable value.
// Returns the string unchanged if it doesn't start with $.
func ExpandEnvVar(s string) string {
	if strings.HasPrefix(s, "$") {
		return os.Getenv(s[1:])
	}
	return s
}
