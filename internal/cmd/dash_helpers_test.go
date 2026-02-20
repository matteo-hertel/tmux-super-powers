package cmd

import (
	"testing"
	"time"
)

func TestInferStatus(t *testing.T) {
	now := time.Now()
	patterns := []string{"FAIL", "panic:", "Error:"}
	promptPattern := `\$\s*$`

	tests := []struct {
		name        string
		prev        string
		current     string
		lastChanged time.Time
		wantStatus  string
	}{
		{
			name:        "active when content changed",
			prev:        "compiling...",
			current:     "tests running...",
			lastChanged: now,
			wantStatus:  "active",
		},
		{
			name:        "idle when unchanged for 30s",
			prev:        "waiting...",
			current:     "waiting...",
			lastChanged: now.Add(-35 * time.Second),
			wantStatus:  "idle",
		},
		{
			name:        "done when prompt visible and idle 60s",
			prev:        "user@host:~$ ",
			current:     "user@host:~$ ",
			lastChanged: now.Add(-65 * time.Second),
			wantStatus:  "done",
		},
		{
			name:        "error when content has error pattern",
			prev:        "running tests...",
			current:     "--- FAIL: TestAuth (0.01s)",
			lastChanged: now,
			wantStatus:  "error",
		},
		{
			name:        "error overrides idle",
			prev:        "--- FAIL: TestAuth",
			current:     "--- FAIL: TestAuth",
			lastChanged: now.Add(-35 * time.Second),
			wantStatus:  "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferStatus(tt.prev, tt.current, tt.lastChanged, now, patterns, promptPattern)
			if got != tt.wantStatus {
				t.Errorf("inferStatus() = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"active", "●"},
		{"idle", "◌"},
		{"done", "✓"},
		{"error", "✗"},
		{"unknown", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := statusIcon(tt.status)
			if got != tt.want {
				t.Errorf("statusIcon(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestFormatTimeSince(t *testing.T) {
	now := time.Now()
	tests := []struct {
		since time.Time
		want  string
	}{
		{now.Add(-2 * time.Second), "2s ago"},
		{now.Add(-3 * time.Minute), "3m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatTimeSince(tt.since, now)
			if got != tt.want {
				t.Errorf("formatTimeSince() = %q, want %q", got, tt.want)
			}
		})
	}
}
