package agentlog

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Parse takes raw JSONL entries and builds display chunks.
func Parse(entries []Entry) []Chunk {
	classified := classify(entries)
	return buildChunks(classified)
}

type classifiedMsg struct {
	role      string
	text      string
	blocks    []ContentBlock
	model     string
	timestamp string
}

func classify(entries []Entry) []classifiedMsg {
	var msgs []classifiedMsg

	for _, e := range entries {
		if isNoise(e) {
			continue
		}
		if e.Message == nil {
			continue
		}

		msg := classifiedMsg{
			role:      e.Message.Role,
			model:     e.Message.Model,
			timestamp: e.Timestamp,
		}

		switch e.Message.Role {
		case "user":
			if e.IsMeta {
				msg.role = "tool_result"
				msg.blocks = extractBlocks(e.Message.Content)
			} else {
				msg.text = extractUserText(e.Message.Content)
				if msg.text == "" || isSystemReminder(msg.text) {
					continue
				}
			}
		case "assistant":
			if e.Message.Model == "<synthetic>" {
				continue
			}
			msg.blocks = extractBlocks(e.Message.Content)
			if len(msg.blocks) == 0 {
				continue
			}
		default:
			continue
		}

		msgs = append(msgs, msg)
	}

	return msgs
}

func isNoise(e Entry) bool {
	switch e.Type {
	case "system", "progress", "file-history-snapshot", "queue-operation":
		return true
	}
	return false
}

func isSystemReminder(text string) bool {
	return strings.Contains(text, "<system-reminder>") ||
		strings.Contains(text, "<local-command-stdout>") ||
		strings.Contains(text, "<local-command-caveat>")
}

func extractUserText(content json.RawMessage) string {
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

func extractBlocks(content json.RawMessage) []ContentBlock {
	var blocks []ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	return blocks
}

func buildChunks(msgs []classifiedMsg) []Chunk {
	var chunks []Chunk
	var assistantBuf []classifiedMsg

	flushAssistant := func() {
		if len(assistantBuf) == 0 {
			return
		}
		chunk := mergeAssistantMsgs(assistantBuf)
		chunks = append(chunks, chunk)
		assistantBuf = nil
	}

	for _, m := range msgs {
		switch m.role {
		case "user":
			flushAssistant()
			chunks = append(chunks, Chunk{
				Type:      "user",
				Text:      m.text,
				Timestamp: m.timestamp,
			})
		case "assistant", "tool_result":
			assistantBuf = append(assistantBuf, m)
		}
	}
	flushAssistant()

	return chunks
}

func mergeAssistantMsgs(msgs []classifiedMsg) Chunk {
	chunk := Chunk{Type: "assistant"}
	pendingTools := make(map[string]*DisplayItem)

	for _, m := range msgs {
		if m.timestamp != "" && chunk.Timestamp == "" {
			chunk.Timestamp = m.timestamp
		}
		if m.model != "" && m.model != "<synthetic>" {
			chunk.Model = m.model
		}

		if m.role == "tool_result" {
			for _, b := range m.blocks {
				if b.Type == "tool_result" && b.ToolUseID != "" {
					if item, ok := pendingTools[b.ToolUseID]; ok {
						item.Result = truncate(extractResultText(b.Content), 500)
						delete(pendingTools, b.ToolUseID)
					}
				}
			}
			continue
		}

		for _, b := range m.blocks {
			switch b.Type {
			case "thinking":
				text := b.Thinking
				if text == "" {
					text = b.Text
				}
				chunk.Items = append(chunk.Items, DisplayItem{
					Type:          "thinking",
					Text:          text,
					TokenEstimate: len(text) / 4,
				})
			case "text":
				if b.Text != "" {
					chunk.Items = append(chunk.Items, DisplayItem{
						Type: "text",
						Text: b.Text,
					})
				}
			case "tool_use":
				if b.Name == "AskUserQuestion" {
					item := parseAskUserQuestion(b)
					chunk.Items = append(chunk.Items, item)
				} else {
					item := DisplayItem{
						Type:    "tool_call",
						Tool:    b.Name,
						Summary: toolSummary(b.Name, b.Input),
						Input:   truncate(string(b.Input), 500),
					}
					chunk.Items = append(chunk.Items, item)
				}
				if b.ID != "" {
					pendingTools[b.ID] = &chunk.Items[len(chunk.Items)-1]
				}
			}
		}
	}

	return chunk
}

// parseAskUserQuestion extracts questions and options from AskUserQuestion tool input.
func parseAskUserQuestion(b ContentBlock) DisplayItem {
	item := DisplayItem{
		Type:    "ask_user",
		Tool:    "AskUserQuestion",
		Summary: "Asking a question",
	}

	var input struct {
		Questions []struct {
			Question    string `json:"question"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}

	if err := json.Unmarshal(b.Input, &input); err != nil {
		return item
	}

	for _, q := range input.Questions {
		qd := AskUserQuestionData{
			Question: q.Question,
		}
		for _, o := range q.Options {
			qd.Options = append(qd.Options, AskUserOption{
				Label:       o.Label,
				Description: o.Description,
			})
		}
		item.Questions = append(item.Questions, qd)
	}

	if len(item.Questions) > 0 {
		item.Summary = truncate(item.Questions[0].Question, 80)
	}

	return item
}

func extractResultText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []ContentBlock
	if json.Unmarshal(content, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(content)
}

func toolSummary(name string, input json.RawMessage) string {
	var m map[string]interface{}
	if err := json.Unmarshal(input, &m); err != nil {
		return name
	}

	switch name {
	case "Read":
		if p, ok := m["file_path"].(string); ok {
			return fmt.Sprintf("Read %s", shortPath(p))
		}
	case "Edit":
		if p, ok := m["file_path"].(string); ok {
			return fmt.Sprintf("Edit %s", shortPath(p))
		}
	case "Write":
		if p, ok := m["file_path"].(string); ok {
			return fmt.Sprintf("Write %s", shortPath(p))
		}
	case "Bash":
		if c, ok := m["command"].(string); ok {
			return fmt.Sprintf("Bash: %s", truncate(c, 60))
		}
	case "Grep":
		if p, ok := m["pattern"].(string); ok {
			return fmt.Sprintf("Grep: %s", truncate(p, 40))
		}
	case "Glob":
		if p, ok := m["pattern"].(string); ok {
			return fmt.Sprintf("Glob: %s", truncate(p, 40))
		}
	case "Agent":
		if p, ok := m["prompt"].(string); ok {
			return fmt.Sprintf("Agent: %s", truncate(p, 50))
		}
	case "WebFetch":
		if u, ok := m["url"].(string); ok {
			return fmt.Sprintf("WebFetch: %s", truncate(u, 50))
		}
	case "WebSearch":
		if q, ok := m["query"].(string); ok {
			return fmt.Sprintf("WebSearch: %s", truncate(q, 50))
		}
	}

	return name
}

func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 3 {
		return p
	}
	return strings.Join(parts[len(parts)-3:], "/")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
