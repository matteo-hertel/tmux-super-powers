package cmd

import "testing"

func TestTaskToBranch(t *testing.T) {
	tests := []struct {
		task string
		want string
	}{
		{"fix the auth token expiry bug", "spawn/fix-the-auth-token-expiry-bug"},
		{"Add Dark Mode Support!", "spawn/add-dark-mode-support"},
		{"refactor: database connection pooling layer", "spawn/refactor-database-connection-pooling-layer"},
		{"", "spawn/task"},
		{"a very long task description that exceeds the fifty character limit for branch names which should be truncated", "spawn/a-very-long-task-description-that-exceeds-the"},
	}
	for _, tt := range tests {
		t.Run(tt.task, func(t *testing.T) {
			got := taskToBranch(tt.task)
			if got != tt.want {
				t.Errorf("taskToBranch(%q) = %q, want %q", tt.task, got, tt.want)
			}
		})
	}
}

func TestParseTaskFile(t *testing.T) {
	input := `# My tasks
fix the authentication bug

add dark mode support
# this is a comment

refactor database layer
`
	tasks := parseTaskFile(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0] != "fix the authentication bug" {
		t.Errorf("task 0: got %q", tasks[0])
	}
	if tasks[1] != "add dark mode support" {
		t.Errorf("task 1: got %q", tasks[1])
	}
	if tasks[2] != "refactor database layer" {
		t.Errorf("task 2: got %q", tasks[2])
	}
}

func TestParseTaskFileEmpty(t *testing.T) {
	tasks := parseTaskFile("# only comments\n\n")
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}
