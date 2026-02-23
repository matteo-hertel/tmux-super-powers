package tmux

import (
	"testing"
)

func TestSanitizeSessionName_Dots(t *testing.T) {
	got := SanitizeSessionName("my.project")
	want := "my-project"
	if got != want {
		t.Errorf("SanitizeSessionName(\"my.project\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Colons(t *testing.T) {
	got := SanitizeSessionName("foo:bar")
	want := "foo-bar"
	if got != want {
		t.Errorf("SanitizeSessionName(\"foo:bar\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Multiple(t *testing.T) {
	got := SanitizeSessionName("my.project:v2.0")
	want := "my-project-v2-0"
	if got != want {
		t.Errorf("SanitizeSessionName(\"my.project:v2.0\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Clean(t *testing.T) {
	got := SanitizeSessionName("my-project")
	want := "my-project"
	if got != want {
		t.Errorf("SanitizeSessionName(\"my-project\") = %q, want %q", got, want)
	}
}

func TestSanitizeSessionName_Empty(t *testing.T) {
	got := SanitizeSessionName("")
	want := ""
	if got != want {
		t.Errorf("SanitizeSessionName(\"\") = %q, want %q", got, want)
	}
}

func TestBuildSessionArgs_NewSession(t *testing.T) {
	args := BuildNewSessionArgs("test-session", "/tmp/dir", "nvim")
	expected := []string{"new-session", "-d", "-s", "test-session", "-c", "/tmp/dir", "nvim"}
	if len(args) != len(expected) {
		t.Fatalf("BuildNewSessionArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildSessionArgs_NoCommand(t *testing.T) {
	args := BuildNewSessionArgs("test-session", "/tmp/dir", "")
	expected := []string{"new-session", "-d", "-s", "test-session", "-c", "/tmp/dir"}
	if len(args) != len(expected) {
		t.Fatalf("BuildNewSessionArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildPopupArgs_DefaultSize(t *testing.T) {
	args := BuildPopupArgs("htop", 75, 75)
	expected := []string{"display-popup", "-E", "-w", "75%", "-h", "75%", "htop"}
	if len(args) != len(expected) {
		t.Fatalf("BuildPopupArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildPopupArgs_CustomSize(t *testing.T) {
	args := BuildPopupArgs("lazydocker", 90, 60)
	expected := []string{"display-popup", "-E", "-w", "90%", "-h", "60%", "lazydocker"}
	if len(args) != len(expected) {
		t.Fatalf("BuildPopupArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestIsInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	if !IsInsideTmux() {
		t.Error("IsInsideTmux() = false, want true")
	}
}

func TestIsInsideTmux_Outside(t *testing.T) {
	t.Setenv("TMUX", "")
	if IsInsideTmux() {
		t.Error("IsInsideTmux() = true, want false")
	}
}

// SendKeys is tested via integration (requires tmux).
// The implementation uses load-buffer/paste-buffer to reliably handle
// arbitrary text including URLs and special characters.

func TestBuildListSessionsArgs(t *testing.T) {
	args := BuildListSessionsArgs()
	if args[0] != "list-sessions" {
		t.Errorf("expected list-sessions, got %s", args[0])
	}
}

func TestBuildCapturePaneArgs(t *testing.T) {
	args := BuildCapturePaneArgs("mysession:0.1")
	expected := []string{"capture-pane", "-t", "mysession:0.1", "-p", "-e"}
	if len(args) != len(expected) {
		t.Fatalf("BuildCapturePaneArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}
