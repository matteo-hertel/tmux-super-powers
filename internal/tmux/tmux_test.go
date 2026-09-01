package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
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
	t.Setenv("SHELL", "/bin/sh")
	args := BuildNewSessionArgs("test-session", "/tmp/dir", "nvim")
	expected := []string{"new-session", "-d", "-s", "test-session", "-c", "/tmp/dir", "/bin/sh", "-c", "nvim"}
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

func TestBuildPreviewCaptureArgs(t *testing.T) {
	args := BuildPreviewCaptureArgs("mysession:0.1")
	expected := []string{"capture-pane", "-t", "mysession:0.1", "-p", "-e", "-S", "-400"}
	if len(args) != len(expected) {
		t.Fatalf("BuildPreviewCaptureArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildPreviewCaptureArgsNeverCapturesWholeScrollback(t *testing.T) {
	args := BuildPreviewCaptureArgs("mysession:0.1")
	for i, a := range args {
		if a == "-S" && i+1 < len(args) && args[i+1] == "-" {
			t.Fatal("preview capture used -S - : the roster reads every pane on each refresh, so an unbounded scrollback freezes the dashboard")
		}
	}
}

func TestBuildTwoPaneSplitArgsUsesTwentyPercentRightPane(t *testing.T) {
	args := BuildTwoPaneSplitArgs("mysession", "/work/project", "")
	expected := []string{"split-window", "-h", "-l", "20%", "-t", "mysession", "-c", "/work/project"}
	if len(args) != len(expected) {
		t.Fatalf("BuildTwoPaneSplitArgs length = %d, want %d", len(args), len(expected))
	}
	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildSplitPaneArgsTargetsParentPane(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	args := BuildSplitPaneArgs("project-task:0.1", "/work/project-task", "claude -p 'check CI'")
	expected := []string{
		"split-window", "-v", "-P", "-F", "#{pane_id}\t#{pane_index}",
		"-t", "project-task:0.1", "-c", "/work/project-task", "/bin/sh", "-c", "claude -p 'check CI'",
	}
	if len(args) != len(expected) {
		t.Fatalf("BuildSplitPaneArgs length = %d, want %d", len(args), len(expected))
	}
	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, arg, expected[i])
		}
	}
}

func TestSplitPaneUsesStableIDAfterPaneIndicesMove(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("SHELL", "/bin/sh")
	session := fmt.Sprintf("tsp-pane-id-test-%d", time.Now().UnixNano())
	if err := CreateTwoPaneSession(session, t.TempDir(), "sleep 5", ""); err != nil {
		t.Fatalf("CreateTwoPaneSession() error = %v", err)
	}
	t.Cleanup(func() {
		_ = KillSession(session)
	})

	delegated, err := SplitPane(session, Pane{Index: 0}, t.TempDir(), "printf 'done\\n'")
	if err != nil {
		t.Fatalf("SplitPane() error = %v", err)
	}
	if delegated.ID == "" {
		t.Fatal("SplitPane() returned an empty pane ID")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && PaneExists(session, delegated) {
		time.Sleep(20 * time.Millisecond)
	}
	if PaneExists(session, delegated) {
		t.Fatal("completed pane still exists by stable ID")
	}
	if !PaneExists(session, Pane{Index: delegated.Index}) {
		t.Fatal("expected the shell pane to reuse the completed pane index")
	}
}

func TestCreateTwoPaneSessionUsesTwosplitLayout(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("SHELL", "/bin/sh")
	session := fmt.Sprintf("tsp-layout-test-%d", time.Now().UnixNano())
	if err := CreateTwoPaneSession(session, t.TempDir(), "sleep 5", ""); err != nil {
		t.Fatalf("CreateTwoPaneSession() error = %v", err)
	}
	t.Cleanup(func() {
		_ = KillSession(session)
	})

	out, err := exec.Command("tmux", "list-panes", "-t", session+":0", "-F", "#{pane_index}|#{pane_width}").Output()
	if err != nil {
		t.Fatalf("list panes: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("pane count = %d, want 2: %q", len(lines), out)
	}
	widths := make(map[int]int, 2)
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			t.Fatalf("invalid pane row: %q", line)
		}
		index, indexErr := strconv.Atoi(parts[0])
		width, widthErr := strconv.Atoi(parts[1])
		if indexErr != nil || widthErr != nil {
			t.Fatalf("invalid pane dimensions: %q", line)
		}
		widths[index] = width
	}
	total := widths[0] + widths[1] + 1
	rightPercent := widths[1] * 100 / total
	if rightPercent < 19 || rightPercent > 21 {
		t.Fatalf("pane widths = %d/%d, right pane = %d%%", widths[0], widths[1], rightPercent)
	}
}
