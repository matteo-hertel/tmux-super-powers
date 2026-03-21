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

// FindJSONLByPrompt searches JSONL files for one whose first user message
// starts with the given prompt prefix. This is used to match a tmux pane's
// Claude Code process (whose command-line prompt is known) to the correct JSONL
// when multiple sessions share the same project directory.
func FindJSONLByPrompt(sessionDir, promptPrefix string) (string, error) {
	if promptPrefix == "" {
		return FindJSONL(sessionDir)
	}
	sessions, err := FindAllJSONL(sessionDir)
	if err != nil {
		return "", err
	}
	// Normalize: take first 100 chars for matching
	prefix := promptPrefix
	if len(prefix) > 100 {
		prefix = prefix[:100]
	}
	for _, s := range sessions {
		firstMsg := readFirstUserMessage(s.Path)
		if firstMsg != "" && len(firstMsg) >= len(prefix) && firstMsg[:len(prefix)] == prefix {
			return s.Path, nil
		}
	}
	return FindJSONL(sessionDir)
}

// readFirstUserMessage reads the first "user" type entry from a JSONL file
// and extracts its text content.
func readFirstUserMessage(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "user" || entry.Message.Role != "user" {
			continue
		}
		// Content can be string or array of blocks
		var text string
		if err := json.Unmarshal(entry.Message.Content, &text); err == nil {
			return text
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(entry.Message.Content, &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" {
					return b.Text
				}
			}
		}
		return ""
	}
	return ""
}

// IsOngoing checks if a JSONL file was modified within the last 2 minutes.
func IsOngoing(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Minute
}
