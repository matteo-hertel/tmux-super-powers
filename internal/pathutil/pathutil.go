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
