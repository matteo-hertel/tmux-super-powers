package cmd

import "testing"

func TestParseLaunchctlPID(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "running with PID",
			output:   "{\n\t\"LimitLoadToSessionType\" = \"Aqua\";\n\t\"Label\" = \"com.tsp.serve\";\n\t\"OnDemand\" = false;\n\t\"LastExitStatus\" = 0;\n\t\"PID\" = 12345;\n\t\"Program\" = \"/usr/local/bin/tsp\";\n};",
			expected: "12345",
		},
		{
			name:     "not running no PID",
			output:   "{\n\t\"LimitLoadToSessionType\" = \"Aqua\";\n\t\"Label\" = \"com.tsp.serve\";\n\t\"OnDemand\" = false;\n\t\"LastExitStatus\" = 256;\n};",
			expected: "",
		},
		{
			name:     "empty output",
			output:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid := parseLaunchctlPID(tt.output)
			if pid != tt.expected {
				t.Errorf("parseLaunchctlPID() = %q, want %q", pid, tt.expected)
			}
		})
	}
}
