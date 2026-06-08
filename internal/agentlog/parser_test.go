package agentlog

import (
	"encoding/json"
	"testing"
)

func TestParseAttachesToolResultAfterAdditionalAssistantItem(t *testing.T) {
	toolInput := json.RawMessage(`{"file_path":"/repo/README.md"}`)
	toolResult := json.RawMessage(`"read contents"`)
	entries := []Entry{
		{
			Type: "assistant",
			Message: &Message{
				Role:  "assistant",
				Model: "claude-opus",
				Content: mustJSON(t, []ContentBlock{
					{Type: "tool_use", ID: "tool-1", Name: "Read", Input: toolInput},
					{Type: "text", Text: "I read the file."},
				}),
			},
		},
		{
			Type:   "user",
			IsMeta: true,
			Message: &Message{
				Role: "user",
				Content: mustJSON(t, []ContentBlock{
					{Type: "tool_result", ToolUseID: "tool-1", Content: toolResult},
				}),
			},
		},
	}

	chunks := Parse(entries)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0].Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(chunks[0].Items))
	}
	if got := chunks[0].Items[0].Result; got != "read contents" {
		t.Fatalf("tool result = %q, want read contents", got)
	}
}

func TestParseAskUserQuestionIncludesSelectionMetadata(t *testing.T) {
	entries := []Entry{
		{
			Type: "assistant",
			Message: &Message{
				Role: "assistant",
				Content: mustJSON(t, []ContentBlock{
					{
						Type:  "tool_use",
						ID:    "ask-1",
						Name:  "AskUserQuestion",
						Input: json.RawMessage(`{"questions":[{"question":"Pick options","multiSelect":true,"freeTextAllowed":true,"options":[{"label":"A","description":"first"},{"label":"B"}]}]}`),
					},
				}),
			},
		},
	}

	chunks := Parse(entries)
	if len(chunks) != 1 || len(chunks[0].Items) != 1 {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
	questions := chunks[0].Items[0].Questions
	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	q := questions[0]
	if !q.MultiSelect {
		t.Fatal("expected multiSelect metadata")
	}
	if !q.FreeTextAllowed {
		t.Fatal("expected freeTextAllowed metadata")
	}
	if len(q.Options) != 2 || q.Options[0].Label != "A" || q.Options[1].Label != "B" {
		t.Fatalf("unexpected options: %#v", q.Options)
	}
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
