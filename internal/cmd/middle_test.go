package cmd

import (
	"testing"
)

func TestMiddleBuildArgs_DefaultSize(t *testing.T) {
	args := buildMiddleArgs("htop", 75, 75)
	expected := []string{"display-popup", "-E", "-w", "75%", "-h", "75%", "htop"}
	if len(args) != len(expected) {
		t.Fatalf("buildMiddleArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestMiddleBuildArgs_CustomSize(t *testing.T) {
	args := buildMiddleArgs("lazydocker", 90, 60)
	expected := []string{"display-popup", "-E", "-w", "90%", "-h", "60%", "lazydocker"}
	if len(args) != len(expected) {
		t.Fatalf("buildMiddleArgs length = %d, want %d", len(args), len(expected))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestMiddleResolveSize_DefaultsOnly(t *testing.T) {
	w, h := resolveMiddleSize(75, 0, 0)
	if w != 75 || h != 75 {
		t.Errorf("resolveMiddleSize(75, 0, 0) = (%d, %d), want (75, 75)", w, h)
	}
}

func TestMiddleResolveSize_WidthOverride(t *testing.T) {
	w, h := resolveMiddleSize(75, 90, 0)
	if w != 90 || h != 75 {
		t.Errorf("resolveMiddleSize(75, 90, 0) = (%d, %d), want (90, 75)", w, h)
	}
}

func TestMiddleResolveSize_HeightOverride(t *testing.T) {
	w, h := resolveMiddleSize(75, 0, 60)
	if w != 75 || h != 60 {
		t.Errorf("resolveMiddleSize(75, 0, 60) = (%d, %d), want (75, 60)", w, h)
	}
}

func TestMiddleResolveSize_BothOverride(t *testing.T) {
	w, h := resolveMiddleSize(75, 90, 60)
	if w != 90 || h != 60 {
		t.Errorf("resolveMiddleSize(75, 90, 60) = (%d, %d), want (90, 60)", w, h)
	}
}
