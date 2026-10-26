package pathutil

import (
	"testing"
)

func TestExpandPath_TildePrefix(t *testing.T) {
	t.Setenv("HOME", "/fake/home")
	got := ExpandPath("~/projects")
	want := "/fake/home/projects"
	if got != want {
		t.Errorf("ExpandPath(\"~/projects\") = %q, want %q", got, want)
	}
}

func TestExpandPath_TildeOnly(t *testing.T) {
	t.Setenv("HOME", "/fake/home")
	got := ExpandPath("~/")
	want := "/fake/home"
	if got != want {
		t.Errorf("ExpandPath(\"~/\") = %q, want %q", got, want)
	}
}

func TestExpandPath_EmptyString(t *testing.T) {
	got := ExpandPath("")
	if got != "" {
		t.Errorf("ExpandPath(\"\") = %q, want \"\"", got)
	}
}

func TestExpandPath_SingleChar(t *testing.T) {
	got := ExpandPath("/")
	if got != "/" {
		t.Errorf("ExpandPath(\"/\") = %q, want \"/\"", got)
	}
}

func TestExpandPath_AbsolutePath(t *testing.T) {
	got := ExpandPath("/usr/local/bin")
	want := "/usr/local/bin"
	if got != want {
		t.Errorf("ExpandPath(\"/usr/local/bin\") = %q, want %q", got, want)
	}
}
