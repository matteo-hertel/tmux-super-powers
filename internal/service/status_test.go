package service

import (
	"testing"
	"time"
)

func TestInferStatus(t *testing.T) {
	now := time.Now()
	errorPatterns := []string{"FAIL", "panic:"}
	promptPattern := `\$\s*$`

	tests := []struct {
		name        string
		prev        string
		current     string
		lastChanged time.Time
		want        string
	}{
		{"error pattern FAIL", "foo", "FAIL: test", now, "error"},
		{"error pattern panic", "foo", "panic: runtime error", now, "error"},
		{"content changed", "old", "new", now, "active"},
		{"recent same content", "same", "same", now.Add(-10 * time.Second), "active"},
		{"idle >30s", "same", "same", now.Add(-40 * time.Second), "idle"},
		{"done >60s with prompt", "same\n$ ", "same\n$ ", now.Add(-90 * time.Second), "done"},
		{"idle >60s no prompt", "same\nno prompt here", "same\nno prompt here", now.Add(-90 * time.Second), "idle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferStatus(tt.prev, tt.current, tt.lastChanged, now, errorPatterns, promptPattern)
			if got != tt.want {
				t.Errorf("InferStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripANSIRemovesTerminalColorSequences(t *testing.T) {
	input := "\x1b[38;5;153mAI_GATEWAY_API_KEY\x1b[39m and \x1b[38;2;248;248;242mtext\x1b[0m\r\n"
	got := StripANSI(input)
	want := "AI_GATEWAY_API_KEY and text\n"
	if got != want {
		t.Fatalf("StripANSI() = %q, want %q", got, want)
	}
}

func TestDetectWaitingPanesDoesNotExposeMenuNavigationHelpAsPrompt(t *testing.T) {
	panes := []Pane{
		{
			Index: 1,
			Type:  "agent",
			Content: "Question?\n" +
				"\x1b[38;5;153mPress Enter to select, press up and down to navigate\x1b[0m\n",
		},
	}

	waiting := DetectWaitingPanes(panes, []string{`Press Enter`})
	if len(waiting) != 1 {
		t.Fatalf("expected one waiting pane, got %d", len(waiting))
	}
	if waiting[0].Prompt != "Question?" {
		t.Fatalf("prompt = %q, want question without navigation help", waiting[0].Prompt)
	}
}

func TestShouldEmitWaitingEventIgnoresTerminalStyleChanges(t *testing.T) {
	prev := &Session{
		Panes: []Pane{
			{
				Index:      1,
				Status:     "waiting",
				Prompt:     "\x1b[38;5;153mPress Enter to select, press up and down to navigate\x1b[0m",
				AgentRunID: "run_1",
			},
		},
	}
	current := Pane{
		Index:      1,
		Status:     "waiting",
		Prompt:     "\x1b[38;5;154mPress Enter to select, press up and down to navigate\x1b[0m",
		AgentRunID: "run_1",
	}

	if shouldEmitWaitingEvent(prev, current) {
		t.Fatal("expected terminal style-only prompt changes to be suppressed")
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"active", "\u25cf"},
		{"idle", "\u25cc"},
		{"done", "\u2713"},
		{"error", "\u2717"},
		{"unknown", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := StatusIcon(tt.status); got != tt.want {
				t.Errorf("StatusIcon(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestFormatTimeSince(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		since time.Time
		want  string
	}{
		{"seconds", now.Add(-30 * time.Second), "30s ago"},
		{"minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"hours", now.Add(-2 * time.Hour), "2h ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTimeSince(tt.since, now); got != tt.want {
				t.Errorf("FormatTimeSince() = %q, want %q", got, tt.want)
			}
		})
	}
}
