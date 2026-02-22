package service

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPaneTypeFromProcess(t *testing.T) {
	tests := []struct {
		process string
		want    string
	}{
		// Editors
		{"nvim", "editor"},
		{"vim", "editor"},
		{"emacs", "editor"},
		{"nano", "editor"},
		// Agent
		{"claude", "agent"},
		// Shells
		{"bash", "shell"},
		{"zsh", "shell"},
		{"fish", "shell"},
		{"sh", "shell"},
		{"", "shell"},
		// Process (everything else)
		{"go", "process"},
		{"node", "process"},
		{"python", "process"},
		{"make", "process"},
		{"cargo", "process"},
	}

	for _, tt := range tests {
		name := tt.process
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got := PaneTypeFromProcess(tt.process)
			if got != tt.want {
				t.Errorf("PaneTypeFromProcess(%q) = %q, want %q", tt.process, got, tt.want)
			}
		})
	}
}

func TestSessionStruct(t *testing.T) {
	now := time.Now()
	s := Session{
		Name:        "my-session",
		Status:      "active",
		Branch:      "feat/api",
		IsWorktree:  true,
		IsGitRepo:   true,
		GitPath:     "/home/user/project",
		LastChanged: now,
		Panes: []Pane{
			{Index: 0, Type: "editor", Process: "nvim"},
			{Index: 1, Type: "agent", Process: "claude", Status: "running"},
		},
		Diff: &DiffStat{
			Files:      3,
			Insertions: 42,
			Deletions:  7,
		},
		PR: &PRInfo{
			Number:      123,
			URL:         "https://github.com/org/repo/pull/123",
			CIStatus:    "pass",
			ReviewCount: 2,
		},
		PrevContent:  "previous output",
		WorktreePath: "/home/user/project-feat-api",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal Session: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify exported JSON fields are present
	expectedFields := []string{"name", "status", "branch", "isWorktree", "isGitRepo", "lastChanged", "panes", "diff", "pr"}
	for _, field := range expectedFields {
		if _, ok := m[field]; !ok {
			t.Errorf("expected JSON field %q not found", field)
		}
	}

	// Verify json:"-" fields are NOT present
	hiddenFields := []string{"GitPath", "gitPath", "PrevContent", "prevContent", "WorktreePath", "worktreePath"}
	for _, field := range hiddenFields {
		if _, ok := m[field]; ok {
			t.Errorf("field %q should not appear in JSON (tagged with json:\"-\")", field)
		}
	}

	// Verify nested Pane fields
	panes, ok := m["panes"].([]interface{})
	if !ok || len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %v", m["panes"])
	}
	pane0 := panes[0].(map[string]interface{})
	if pane0["type"] != "editor" {
		t.Errorf("pane[0].type = %q, want %q", pane0["type"], "editor")
	}
	if pane0["process"] != "nvim" {
		t.Errorf("pane[0].process = %q, want %q", pane0["process"], "nvim")
	}

	// Verify DiffStat
	diff := m["diff"].(map[string]interface{})
	if int(diff["files"].(float64)) != 3 {
		t.Errorf("diff.files = %v, want 3", diff["files"])
	}
	if int(diff["insertions"].(float64)) != 42 {
		t.Errorf("diff.insertions = %v, want 42", diff["insertions"])
	}
	if int(diff["deletions"].(float64)) != 7 {
		t.Errorf("diff.deletions = %v, want 7", diff["deletions"])
	}

	// Verify PRInfo
	pr := m["pr"].(map[string]interface{})
	if int(pr["number"].(float64)) != 123 {
		t.Errorf("pr.number = %v, want 123", pr["number"])
	}
	if pr["url"] != "https://github.com/org/repo/pull/123" {
		t.Errorf("pr.url = %v, want expected URL", pr["url"])
	}
}

func TestSessionStructOmitempty(t *testing.T) {
	s := Session{
		Name:   "minimal",
		Status: "idle",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal Session: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Fields with omitempty should be absent when zero
	if _, ok := m["branch"]; ok {
		t.Error("branch should be omitted when empty")
	}
	if _, ok := m["diff"]; ok {
		t.Error("diff should be omitted when nil")
	}
	if _, ok := m["pr"]; ok {
		t.Error("pr should be omitted when nil")
	}
}

func TestMarshalSessions(t *testing.T) {
	sessions := []Session{
		{Name: "s1", Status: "active"},
		{Name: "s2", Status: "idle"},
	}

	data, err := MarshalSessions(sessions)
	if err != nil {
		t.Fatalf("MarshalSessions failed: %v", err)
	}

	var envelope map[string][]Session
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	got, ok := envelope["sessions"]
	if !ok {
		t.Fatal("expected top-level 'sessions' key")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}
	if got[0].Name != "s1" {
		t.Errorf("sessions[0].Name = %q, want %q", got[0].Name, "s1")
	}
	if got[1].Name != "s2" {
		t.Errorf("sessions[1].Name = %q, want %q", got[1].Name, "s2")
	}
}

func TestMarshalSessionsEmpty(t *testing.T) {
	data, err := MarshalSessions([]Session{})
	if err != nil {
		t.Fatalf("MarshalSessions failed: %v", err)
	}

	var envelope map[string][]Session
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	got, ok := envelope["sessions"]
	if !ok {
		t.Fatal("expected top-level 'sessions' key")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(got))
	}
}
