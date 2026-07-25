package service

import (
	"testing"
)

func TestPaneTypeFromProcess(t *testing.T) {
	tests := []struct {
		process  string
		expected string
	}{
		{"nvim", "editor"},
		{"vim", "editor"},
		{"emacs", "editor"},
		{"nano", "editor"},
		{"claude", "agent"},
		{"aider", "agent"},
		{"codex", "agent"},
		{"codex-aarch64-a", "agent"},
		{"bash", "shell"},
		{"zsh", "shell"},
		{"fish", "shell"},
		{"sh", "shell"},
		{"", "shell"},
		{"2.1.71", "agent"}, // Claude Code version
		{"2.1.81", "agent"}, // newer version
		{"3.0.0", "agent"},  // future major
		{"node", "process"},
		{"python3", "process"},
		{"go", "process"},
		{"2.1.71-rc1", "process"}, // non-standard version — not agent
		{"v2.1.71", "process"},    // prefixed — not agent
	}
	for _, tt := range tests {
		name := tt.process
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			result := PaneTypeFromProcess(tt.process)
			if result != tt.expected {
				t.Errorf("PaneTypeFromProcess(%q) = %q, want %q", tt.process, result, tt.expected)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	got := StripANSI("\x1b[32mrunning\x1b[0m\r\n")
	if got != "running\n" {
		t.Fatalf("StripANSI() = %q, want %q", got, "running\n")
	}
}
