package cmd

import "testing"

func TestShouldIgnoreDir_Hidden(t *testing.T) {
	if !shouldIgnoreDir(".git", map[string]bool{}) {
		t.Error("shouldIgnoreDir(\".git\") = false, want true")
	}
}

func TestShouldIgnoreDir_UserIgnore(t *testing.T) {
	ignores := map[string]bool{"node_modules": true}
	if !shouldIgnoreDir("node_modules", ignores) {
		t.Error("shouldIgnoreDir(\"node_modules\") = false, want true")
	}
}

func TestShouldIgnoreDir_CaseInsensitive(t *testing.T) {
	ignores := map[string]bool{"node_modules": true}
	if !shouldIgnoreDir("Node_Modules", ignores) {
		t.Error("shouldIgnoreDir(\"Node_Modules\") with lowercase key = false, want true")
	}
}

func TestShouldIgnoreDir_Normal(t *testing.T) {
	if shouldIgnoreDir("src", map[string]bool{}) {
		t.Error("shouldIgnoreDir(\"src\") = true, want false")
	}
}

func TestBuildIgnoreSet_Empty(t *testing.T) {
	set := buildIgnoreSet(nil)
	if len(set) != 0 {
		t.Errorf("buildIgnoreSet(nil) length = %d, want 0", len(set))
	}
}

func TestBuildIgnoreSet_WithEntries(t *testing.T) {
	set := buildIgnoreSet([]string{"node_modules", "vendor"})
	if !set["node_modules"] {
		t.Error("expected node_modules in set")
	}
	if !set["vendor"] {
		t.Error("expected vendor in set")
	}
}
