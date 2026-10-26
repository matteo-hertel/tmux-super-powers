package service

import (
	"testing"
)

func TestTaskToBranch(t *testing.T) {
	tests := []struct {
		task string
		want string
	}{
		{"fix the auth bug", "spawn/fix-the-auth-bug"},
		{"", "spawn/task"},
		{"Add Dark Mode!!!", "spawn/add-dark-mode"},
		{"UPPERCASE task", "spawn/uppercase-task"},
		{"special chars: @#$%", "spawn/special-chars"},
		{"multiple   spaces", "spawn/multiple-spaces"},
	}
	for _, tt := range tests {
		t.Run(tt.task, func(t *testing.T) {
			got := TaskToBranch(tt.task)
			if got != tt.want {
				t.Errorf("TaskToBranch(%q) = %q, want %q", tt.task, got, tt.want)
			}
		})
	}
}

func TestBuildLaunchCommand(t *testing.T) {
	agent := "claude 'fix the bug'"
	fallback := "; fi"
	tests := []struct {
		name    string
		install string
		setup   string
		want    string
	}{
		{"no install or setup", "", "", agent},
		{"install only", "yarn install", "", "if ( yarn install ); then " + agent + fallback},
		{"setup only", "", "make deps", "if ( make deps ); then " + agent + fallback},
		{"install then setup", "pnpm install", "make deps", "if ( pnpm install ) && ( make deps ); then " + agent + fallback},
		{"multi-command setup stays grouped", "npm install", "a; b", "if ( npm install ) && ( a; b ); then " + agent + fallback},
		{"blank steps ignored", "  ", "\t", agent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLaunchCommand(tt.install, tt.setup, agent)
			if got != tt.want {
				t.Errorf("buildLaunchCommand(%q, %q) = %q, want %q", tt.install, tt.setup, got, tt.want)
			}
		})
	}
}

func TestTaskToBranchTruncation(t *testing.T) {
	long := "a very long task name that exceeds the fifty character limit for branch names"
	got := TaskToBranch(long)
	branch := got[len("spawn/"):]
	if len(branch) > 50 {
		t.Errorf("branch name too long: %d chars", len(branch))
	}
	if branch[len(branch)-1] == '-' {
		t.Error("branch name should not end with hyphen")
	}
}

func TestBuildAgentCommandQuotesPrompt(t *testing.T) {
	got := BuildAgentCommand("claude -p", "fix Matt's test")
	want := "claude -p 'fix Matt'\\''s test'"
	if got != want {
		t.Fatalf("BuildAgentCommand() = %q, want %q", got, want)
	}
}
