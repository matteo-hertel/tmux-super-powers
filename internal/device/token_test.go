package device

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	tok := GenerateToken()

	if !strings.HasPrefix(tok, "tsp_") {
		t.Errorf("expected prefix tsp_, got %s", tok)
	}

	// tsp_ (4) + 32 hex chars = 36
	if len(tok) != 36 {
		t.Errorf("expected length 36, got %d (%s)", len(tok), tok)
	}
}

func TestGenerateToken_Unique(t *testing.T) {
	a := GenerateToken()
	b := GenerateToken()

	if a == b {
		t.Errorf("expected two unique tokens, both were %s", a)
	}
}

func TestGenerateDeviceID(t *testing.T) {
	id := GenerateDeviceID()

	if !strings.HasPrefix(id, "d_") {
		t.Errorf("expected prefix d_, got %s", id)
	}

	// d_ (2) + 12 hex chars = 14
	if len(id) != 14 {
		t.Errorf("expected length 14, got %d (%s)", len(id), id)
	}
}

func TestGenerateAdminToken(t *testing.T) {
	tok := GenerateAdminToken()

	if !strings.HasPrefix(tok, "tsp_admin_") {
		t.Errorf("expected prefix tsp_admin_, got %s", tok)
	}

	// tsp_admin_ (10) + 32 hex chars = 42
	if len(tok) != 42 {
		t.Errorf("expected length 42, got %d (%s)", len(tok), tok)
	}
}

func TestGeneratePairingCode(t *testing.T) {
	code := GeneratePairingCode()

	if len(code) != 6 {
		t.Fatalf("expected length 6, got %d (%s)", len(code), code)
	}

	allowed := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for _, c := range code {
		if !strings.ContainsRune(allowed, c) {
			t.Errorf("character %c not in allowed set %s", c, allowed)
		}
	}
}
