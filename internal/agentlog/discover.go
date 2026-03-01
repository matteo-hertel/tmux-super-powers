package agentlog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FindJSONL finds the most recent Claude Code JSONL file for a session directory.
func FindJSONL(sessionDir string) (string, error) {
	if sessionDir == "" {
		return "", os.ErrNotExist
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Claude Code encodes project path: replace "/" with "-"
	encoded := strings.ReplaceAll(sessionDir, "/", "-")
	projectDir := filepath.Join(homeDir, ".claude", "projects", encoded)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", err
	}

	type jsonlFile struct {
		path    string
		modTime int64
	}

	var files []jsonlFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, jsonlFile{
			path:    filepath.Join(projectDir, e.Name()),
			modTime: info.ModTime().UnixMilli(),
		})
	}

	if len(files) == 0 {
		return "", os.ErrNotExist
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	return files[0].path, nil
}

// IsOngoing checks if a JSONL file was modified within the last 2 minutes.
func IsOngoing(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Minute
}
