package agentlog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AgentSession describes a single Claude Code JSONL log file.
type AgentSession struct {
	ID      string `json:"id"`      // filename without extension
	Path    string `json:"-"`       // full path (not exposed to API)
	Ongoing bool   `json:"ongoing"` // modified within last 2 minutes
	ModTime int64  `json:"modTime"` // unix millis
}

// FindAllJSONL returns all Claude Code JSONL files for a session directory,
// sorted by modification time (most recent first).
func FindAllJSONL(sessionDir string) ([]AgentSession, error) {
	if sessionDir == "" {
		return nil, os.ErrNotExist
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	encoded := strings.ReplaceAll(sessionDir, "/", "-")
	projectDir := filepath.Join(homeDir, ".claude", "projects", encoded)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	var sessions []AgentSession
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(projectDir, e.Name())
		sessions = append(sessions, AgentSession{
			ID:      strings.TrimSuffix(e.Name(), ".jsonl"),
			Path:    fullPath,
			Ongoing: IsOngoing(fullPath),
			ModTime: info.ModTime().UnixMilli(),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime > sessions[j].ModTime
	})

	return sessions, nil
}

// FindJSONL finds the most recent Claude Code JSONL file for a session directory.
func FindJSONL(sessionDir string) (string, error) {
	sessions, err := FindAllJSONL(sessionDir)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", os.ErrNotExist
	}
	// Prefer ongoing sessions over stale ones.
	for _, s := range sessions {
		if s.Ongoing {
			return s.Path, nil
		}
	}
	return sessions[0].Path, nil
}

// IsOngoing checks if a JSONL file was modified within the last 2 minutes.
func IsOngoing(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Minute
}
