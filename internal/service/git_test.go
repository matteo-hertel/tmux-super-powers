package service

import (
	"strings"
	"testing"
)

func TestParseDiffStat(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantFiles  int
		wantInsert int
		wantDelete int
	}{
		{
			name:       "3 files with insertions and deletions",
			input:      " 3 files changed, 45 insertions(+), 12 deletions(-)\n",
			wantFiles:  3,
			wantInsert: 45,
			wantDelete: 12,
		},
		{
			name:       "1 file with insertions and deletions",
			input:      " 1 file changed, 10 insertions(+), 2 deletions(-)\n",
			wantFiles:  1,
			wantInsert: 10,
			wantDelete: 2,
		},
		{
			name:       "insertions only",
			input:      " 2 files changed, 30 insertions(+)\n",
			wantFiles:  2,
			wantInsert: 30,
			wantDelete: 0,
		},
		{
			name:       "deletions only",
			input:      " 5 files changed, 7 deletions(-)\n",
			wantFiles:  5,
			wantInsert: 0,
			wantDelete: 7,
		},
		{
			name:       "no match",
			input:      "some random text that does not match",
			wantFiles:  0,
			wantInsert: 0,
			wantDelete: 0,
		},
		{
			name:       "empty string",
			input:      "",
			wantFiles:  0,
			wantInsert: 0,
			wantDelete: 0,
		},
		{
			name:       "1 file singular insertions and deletions",
			input:      " 1 file changed, 1 insertion(+), 1 deletion(-)\n",
			wantFiles:  1,
			wantInsert: 1,
			wantDelete: 1,
		},
		{
			name: "stat output with file list",
			input: ` internal/cmd/dash.go | 50 +++++++++++++++++++++++++---------
 internal/cmd/root.go |  3 ++-
 2 files changed, 35 insertions(+), 18 deletions(-)
`,
			wantFiles:  2,
			wantInsert: 35,
			wantDelete: 18,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, insertions, deletions := ParseDiffStat(tt.input)
			if files != tt.wantFiles {
				t.Errorf("files = %d, want %d", files, tt.wantFiles)
			}
			if insertions != tt.wantInsert {
				t.Errorf("insertions = %d, want %d", insertions, tt.wantInsert)
			}
			if deletions != tt.wantDelete {
				t.Errorf("deletions = %d, want %d", deletions, tt.wantDelete)
			}
		})
	}
}

func TestFormatPRComments(t *testing.T) {
	t.Run("groups comments by file", func(t *testing.T) {
		comments := []PRComment{
			{File: "main.go", Line: 10, Author: "alice", Body: "Fix this"},
			{File: "main.go", Line: 20, Author: "bob", Body: "Also fix this"},
			{File: "utils.go", Line: 5, Author: "alice", Body: "Rename this"},
		}

		result := FormatPRComments(comments)

		// Check header
		if !strings.Contains(result, "## PR Review Comments") {
			t.Error("missing header")
		}

		// Check file groupings
		if !strings.Contains(result, "### main.go") {
			t.Error("missing main.go heading")
		}
		if !strings.Contains(result, "### utils.go") {
			t.Error("missing utils.go heading")
		}

		// Check comment formatting
		if !strings.Contains(result, `Line 10 — @alice: "Fix this"`) {
			t.Error("missing alice's comment on line 10")
		}
		if !strings.Contains(result, `Line 20 — @bob: "Also fix this"`) {
			t.Error("missing bob's comment on line 20")
		}
		if !strings.Contains(result, `Line 5 — @alice: "Rename this"`) {
			t.Error("missing alice's comment on utils.go")
		}

		// Check that main.go comes before utils.go (preserves insertion order)
		mainIdx := strings.Index(result, "### main.go")
		utilsIdx := strings.Index(result, "### utils.go")
		if mainIdx >= utilsIdx {
			t.Error("expected main.go to appear before utils.go (insertion order)")
		}
	})

	t.Run("empty comments", func(t *testing.T) {
		result := FormatPRComments(nil)
		if !strings.Contains(result, "## PR Review Comments") {
			t.Error("missing header for empty comments")
		}
		// Should still have the header, just no file sections
		if strings.Contains(result, "###") {
			t.Error("should not have file sections for empty comments")
		}
	})

	t.Run("single file single comment", func(t *testing.T) {
		comments := []PRComment{
			{File: "cmd/root.go", Line: 42, Author: "reviewer", Body: "Nice work"},
		}
		result := FormatPRComments(comments)
		if !strings.Contains(result, "### cmd/root.go") {
			t.Error("missing file heading")
		}
		if !strings.Contains(result, `Line 42 — @reviewer: "Nice work"`) {
			t.Error("missing comment")
		}
	})
}
