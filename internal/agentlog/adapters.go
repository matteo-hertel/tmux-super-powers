package agentlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func ReadRunLog(ref RunLogRef, offset int64) (AgentLogResponse, error) {
	switch ref.Provider {
	case "claude":
		return readClaudeRunLog(ref, offset)
	case "codex":
		return readCodexRunLog(ref, offset)
	default:
		return readFallbackRunLog(ref, offset)
	}
}

func readClaudeRunLog(ref RunLogRef, offset int64) (AgentLogResponse, error) {
	if ref.LogPath == "" {
		return AgentLogResponse{}, fmt.Errorf("agent log path is unknown")
	}
	entries, newOffset, err := ReadEntriesFrom(ref.LogPath, offset)
	if err != nil {
		return AgentLogResponse{}, err
	}
	return AgentLogResponse{
		RunID:      ref.ID,
		Provider:   ref.Provider,
		Chunks:     Parse(entries),
		Ongoing:    IsOngoing(ref.LogPath),
		ByteOffset: newOffset,
	}, nil
}

func readCodexRunLog(ref RunLogRef, offset int64) (AgentLogResponse, error) {
	if ref.LogPath == "" {
		return AgentLogResponse{}, fmt.Errorf("agent log path is unknown")
	}
	lines, newOffset, err := readLinesFrom(ref.LogPath, offset)
	if err != nil {
		return AgentLogResponse{}, err
	}
	var chunks []Chunk
	for _, line := range lines {
		if chunk, ok := parseCodexLine(line); ok {
			chunks = append(chunks, chunk)
		}
	}
	return AgentLogResponse{
		RunID:      ref.ID,
		Provider:   ref.Provider,
		Chunks:     chunks,
		Ongoing:    IsOngoing(ref.LogPath),
		ByteOffset: newOffset,
	}, nil
}

func readFallbackRunLog(ref RunLogRef, offset int64) (AgentLogResponse, error) {
	if ref.LogPath == "" {
		return AgentLogResponse{}, fmt.Errorf("agent log path is unknown")
	}
	return readCodexRunLog(ref, offset)
}

func readLinesFrom(path string, offset int64) ([]json.RawMessage, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, 0, err
		}
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)
	var lines []json.RawMessage
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}
	info, statErr := f.Stat()
	var finalOffset int64
	if statErr == nil {
		finalOffset = info.Size()
	}
	return lines, finalOffset, scanner.Err()
}

func parseCodexLine(line json.RawMessage) (Chunk, bool) {
	var entry struct {
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &entry); err != nil {
		return Chunk{}, false
	}
	if entry.Type == "response_item" {
		if chunk, ok := parseCodexResponseItem(entry.Timestamp, entry.Payload); ok {
			return chunk, true
		}
	}
	if entry.Type == "event_msg" {
		if chunk, ok := parseCodexEventMsg(entry.Timestamp, entry.Payload); ok {
			return chunk, true
		}
	}
	return Chunk{}, false
}

func parseCodexResponseItem(timestamp string, payload json.RawMessage) (Chunk, bool) {
	var msg struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil || msg.Type != "message" {
		return Chunk{}, false
	}
	text := codexContentText(msg.Content)
	if strings.TrimSpace(text) == "" {
		return Chunk{}, false
	}
	role := msg.Role
	if role != "user" {
		role = "assistant"
	}
	return Chunk{Type: role, Text: text, Timestamp: timestamp}, true
}

func codexContentText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var parts []string
	for _, c := range content {
		switch c.Type {
		case "input_text", "output_text", "text":
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func parseCodexEventMsg(timestamp string, payload json.RawMessage) (Chunk, bool) {
	var ev struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return Chunk{}, false
	}
	if strings.TrimSpace(ev.Message) == "" {
		return Chunk{}, false
	}
	role := "event"
	switch ev.Type {
	case "user_message":
		role = "user"
	case "agent_message", "assistant_message":
		role = "assistant"
	}
	return Chunk{Type: role, Text: ev.Message, Timestamp: timestamp}, true
}
