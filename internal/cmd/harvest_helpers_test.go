package cmd

import (
	"strings"
	"testing"
)

func TestParseDiffStat(t *testing.T) {
	input := ` src/auth.go   | 12 ++++++------
 src/db.go     |  4 ++--
 2 files changed, 8 insertions(+), 8 deletions(-)`

	files, ins, del := parseDiffStat(input)
	if files != 2 {
		t.Errorf("files: got %d, want 2", files)
	}
	if ins != 8 {
		t.Errorf("insertions: got %d, want 8", ins)
	}
	if del != 8 {
		t.Errorf("deletions: got %d, want 8", del)
	}
}

func TestParseDiffStatEmpty(t *testing.T) {
	files, ins, del := parseDiffStat("")
	if files != 0 || ins != 0 || del != 0 {
		t.Errorf("expected all zeros, got %d %d %d", files, ins, del)
	}
}

func TestFormatPRComments(t *testing.T) {
	comments := []prComment{
		{File: "src/auth.go", Line: 45, Author: "reviewer", Body: "Handle empty token"},
		{File: "src/auth.go", Line: 78, Author: "reviewer", Body: "Use a constant"},
		{File: "src/db.go", Line: 10, Author: "other", Body: "Good catch"},
	}
	result := formatPRComments(comments)
	if !strings.Contains(result, "src/auth.go") {
		t.Error("expected auth.go in output")
	}
	if !strings.Contains(result, "Handle empty token") {
		t.Error("expected comment body in output")
	}
}
