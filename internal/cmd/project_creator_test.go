package cmd

import "testing"

func TestSessionLaunchSummaryShowsPaneCommands(t *testing.T) {
	got := sessionLaunchSummary("project-tsp", "nvim", "")
	want := "Starting tmux session project-tsp\n  pane 0: nvim\n  pane 1: $SHELL\n"
	if got != want {
		t.Fatalf("sessionLaunchSummary() = %q, want %q", got, want)
	}
}
