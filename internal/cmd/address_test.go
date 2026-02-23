package cmd

import "testing"

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		input    string
		port     int
		expected string
	}{
		// Bare hostname → prepend http://, append port
		{"my-machine.tail1234.ts.net", 7777, "http://my-machine.tail1234.ts.net:7777"},
		// Hostname with port → prepend http://, keep port
		{"my-machine.tail1234.ts.net:8080", 7777, "http://my-machine.tail1234.ts.net:8080"},
		// Full URL → use as-is
		{"http://10.0.0.1:7777", 7777, "http://10.0.0.1:7777"},
		// Full URL with different port → use as-is
		{"http://10.0.0.1:9999", 7777, "http://10.0.0.1:9999"},
		// IP only → prepend http://, append port
		{"100.68.1.42", 7777, "http://100.68.1.42:7777"},
		// IP with port → prepend http://, keep port
		{"100.68.1.42:8080", 7777, "http://100.68.1.42:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeAddress(tt.input, tt.port)
			if result != tt.expected {
				t.Errorf("normalizeAddress(%q, %d) = %q, want %q", tt.input, tt.port, result, tt.expected)
			}
		})
	}
}
