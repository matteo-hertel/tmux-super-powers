package agentlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCodexLogPassthroughMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex.jsonl")
	data := "" +
		`{"timestamp":"2026-05-14T10:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}` + "\n" +
		`{"timestamp":"2026-05-14T10:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]}}` + "\n" +
		`{"timestamp":"2026-05-14T10:00:02Z","type":"event_msg","payload":{"type":"agent_message","message":"working"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	resp, err := ReadRunLog(RunLogRef{ID: "run_codex", Provider: "codex", LogPath: path}, 0)
	if err != nil {
		t.Fatalf("ReadRunLog: %v", err)
	}
	if resp.RunID != "run_codex" || resp.Provider != "codex" {
		t.Fatalf("unexpected response identity: run=%q provider=%q", resp.RunID, resp.Provider)
	}
	if len(resp.Chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %#v", len(resp.Chunks), resp.Chunks)
	}
	if resp.Chunks[0].Type != "user" || resp.Chunks[0].Text != "hello" {
		t.Fatalf("unexpected user chunk: %#v", resp.Chunks[0])
	}
	if resp.Chunks[1].Type != "assistant" || resp.Chunks[1].Text != "hi there" {
		t.Fatalf("unexpected assistant chunk: %#v", resp.Chunks[1])
	}
	if resp.Chunks[2].Type != "assistant" || resp.Chunks[2].Text != "working" {
		t.Fatalf("unexpected event chunk: %#v", resp.Chunks[2])
	}
}

func TestReadCodexLogSkipsNoisyInternalEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex.jsonl")
	data := "" +
		`{"timestamp":"2026-05-14T10:00:00Z","type":"session_meta","payload":{"id":"abc","cwd":"/repo","base_instructions":{"text":"large"}}}` + "\n" +
		`{"timestamp":"2026-05-14T10:00:01Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}"}}` + "\n" +
		`{"timestamp":"2026-05-14T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_tokens":100}}}` + "\n" +
		`{"timestamp":"2026-05-14T10:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	resp, err := ReadRunLog(RunLogRef{ID: "run_codex", Provider: "codex", LogPath: path}, 0)
	if err != nil {
		t.Fatalf("ReadRunLog: %v", err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("expected only semantic chunks, got %d: %#v", len(resp.Chunks), resp.Chunks)
	}
	if resp.Chunks[0].Type != "assistant" || resp.Chunks[0].Text != "done" {
		t.Fatalf("unexpected remaining chunk: %#v", resp.Chunks[0])
	}
}
