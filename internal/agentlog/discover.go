package agentlog

import (
	"bufio"
	"encoding/json"
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

// FindJSONLByID finds a Claude Code JSONL file by session UUID.
func FindJSONLByID(sessionDir, id string) (AgentSession, bool) {
	sessions, err := FindAllJSONL(sessionDir)
	if err != nil {
		return AgentSession{}, false
	}
	for _, s := range sessions {
		if s.ID == id {
			return s, true
		}
	}
	return AgentSession{}, false
}

// FindCodexJSONL returns the newest Codex rollout log whose session metadata cwd
// matches sessionDir. The Codex format is still evolving, so this is a
// low-confidence discovery path used after process-based run detection.
func FindCodexJSONL(sessionDir string) (AgentSession, bool) {
	sessions, err := FindAllCodexJSONL(sessionDir)
	if err != nil || len(sessions) == 0 {
		return AgentSession{}, false
	}
	return sessions[0], true
}

func FindAllCodexJSONL(sessionDir string) ([]AgentSession, error) {
	if sessionDir == "" {
		return nil, os.ErrNotExist
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sessionDir = filepath.Clean(sessionDir)
	roots := []string{
		filepath.Join(homeDir, ".codex", "sessions"),
		filepath.Join(homeDir, ".codex", "archived_sessions"),
	}
	var sessions []AgentSession
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			id, cwd, ok := readCodexSessionMeta(path)
			if !ok || filepath.Clean(cwd) != sessionDir {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if id == "" {
				id = strings.TrimSuffix(d.Name(), ".jsonl")
			}
			sessions = append(sessions, AgentSession{
				ID:      id,
				Path:    path,
				Ongoing: IsOngoing(path),
				ModTime: info.ModTime().UnixMilli(),
			})
			return nil
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime > sessions[j].ModTime
	})
	return sessions, nil
}

func readCodexSessionMeta(path string) (id string, cwd string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "session_meta" {
			return entry.Payload.ID, entry.Payload.CWD, entry.Payload.CWD != ""
		}
	}
	return "", "", false
}

// IsOngoing checks if a JSONL file was modified within the last 2 minutes.
func IsOngoing(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Minute
}
