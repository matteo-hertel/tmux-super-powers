package device

import (
	"errors"
	"testing"
	"time"
)

func TestPairingManager_Initiate(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	code, err := pm.Initiate("iPhone")
	if err != nil {
		t.Fatalf("Initiate: unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("expected 6-char code, got %d chars: %s", len(code), code)
	}
}

func TestPairingManager_Complete(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	code, err := pm.Initiate("iPhone")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	name, err := pm.Complete(code)
	if err != nil {
		t.Fatalf("Complete: unexpected error: %v", err)
	}
	if name != "iPhone" {
		t.Errorf("expected device name 'iPhone', got %q", name)
	}
}

func TestPairingManager_CompleteInvalidCode(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	// No initiate at all
	_, err := pm.Complete("ZZZZZZ")
	if !errors.Is(err, ErrInvalidCode) {
		t.Errorf("expected ErrInvalidCode, got %v", err)
	}

	// Initiate, then try wrong code
	_, _ = pm.Initiate("iPhone")
	_, err = pm.Complete("BADCOD")
	if !errors.Is(err, ErrInvalidCode) {
		t.Errorf("expected ErrInvalidCode for wrong code, got %v", err)
	}
}

func TestPairingManager_CompleteExpired(t *testing.T) {
	pm := NewPairingManager(1 * time.Millisecond)

	code, err := pm.Initiate("iPhone")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	_, err = pm.Complete(code)
	if !errors.Is(err, ErrCodeExpired) {
		t.Errorf("expected ErrCodeExpired, got %v", err)
	}
}

func TestPairingManager_CompleteAlreadyUsed(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	code, err := pm.Initiate("iPhone")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	_, err = pm.Complete(code)
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}

	_, err = pm.Complete(code)
	if !errors.Is(err, ErrCodeUsed) {
		t.Errorf("expected ErrCodeUsed on second Complete, got %v", err)
	}
}

func TestPairingManager_NewInitiateInvalidatesPrevious(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	code1, err := pm.Initiate("iPhone")
	if err != nil {
		t.Fatalf("first Initiate: %v", err)
	}

	code2, err := pm.Initiate("iPad")
	if err != nil {
		t.Fatalf("second Initiate: %v", err)
	}

	// Old code should be invalid
	_, err = pm.Complete(code1)
	if !errors.Is(err, ErrInvalidCode) {
		t.Errorf("expected ErrInvalidCode for old code, got %v", err)
	}

	// New code should work
	name, err := pm.Complete(code2)
	if err != nil {
		t.Fatalf("Complete with new code: %v", err)
	}
	if name != "iPad" {
		t.Errorf("expected device name 'iPad', got %q", name)
	}
}

func TestPairingManager_Status(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	code, err := pm.Initiate("iPhone")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	// Before claiming
	claimed, name := pm.Status(code)
	if claimed {
		t.Error("expected claimed=false before Complete")
	}
	if name != "iPhone" {
		t.Errorf("expected device name 'iPhone', got %q", name)
	}

	// After claiming
	_, err = pm.Complete(code)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	claimed, name = pm.Status(code)
	if !claimed {
		t.Error("expected claimed=true after Complete")
	}
	if name != "iPhone" {
		t.Errorf("expected device name 'iPhone', got %q", name)
	}

	// Unknown code
	claimed, name = pm.Status("ZZZZZZ")
	if claimed {
		t.Error("expected claimed=false for unknown code")
	}
	if name != "" {
		t.Errorf("expected empty name for unknown code, got %q", name)
	}
}
