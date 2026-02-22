package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_AddAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s := NewStore(path)

	d1 := Device{
		ID:       "dev-1",
		Name:     "iPhone",
		Token:    "tok-aaa",
		PairedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	d2 := Device{
		ID:       "dev-2",
		Name:     "iPad",
		Token:    "tok-bbb",
		PairedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := s.Add(d1); err != nil {
		t.Fatalf("Add d1: %v", err)
	}
	if err := s.Add(d2); err != nil {
		t.Fatalf("Add d2: %v", err)
	}

	devices, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	if devices[0].ID != "dev-1" {
		t.Errorf("expected first device ID dev-1, got %s", devices[0].ID)
	}
	if devices[1].ID != "dev-2" {
		t.Errorf("expected second device ID dev-2, got %s", devices[1].ID)
	}
}

func TestStore_FindByToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s := NewStore(path)

	d := Device{
		ID:       "dev-1",
		Name:     "iPhone",
		Token:    "tok-aaa",
		PairedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := s.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Found
	found := s.FindByToken("tok-aaa")
	if found == nil {
		t.Fatal("expected to find device by token, got nil")
	}
	if found.ID != "dev-1" {
		t.Errorf("expected ID dev-1, got %s", found.ID)
	}
	if found.Name != "iPhone" {
		t.Errorf("expected Name iPhone, got %s", found.Name)
	}

	// Not found
	notFound := s.FindByToken("tok-zzz")
	if notFound != nil {
		t.Errorf("expected nil for unknown token, got %+v", notFound)
	}
}

func TestStore_Remove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s := NewStore(path)

	d1 := Device{ID: "dev-1", Name: "iPhone", Token: "tok-aaa", PairedAt: time.Now()}
	d2 := Device{ID: "dev-2", Name: "iPad", Token: "tok-bbb", PairedAt: time.Now()}

	if err := s.Add(d1); err != nil {
		t.Fatalf("Add d1: %v", err)
	}
	if err := s.Add(d2); err != nil {
		t.Fatalf("Add d2: %v", err)
	}

	if err := s.Remove("dev-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	devices, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device after remove, got %d", len(devices))
	}
	if devices[0].ID != "dev-2" {
		t.Errorf("expected remaining device dev-2, got %s", devices[0].ID)
	}
}

func TestStore_UpdateLastSeen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s := NewStore(path)

	d := Device{ID: "dev-1", Name: "iPhone", Token: "tok-aaa", PairedAt: time.Now()}
	if err := s.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}

	now := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
	s.UpdateLastSeen("tok-aaa", now)

	found := s.FindByToken("tok-aaa")
	if found == nil {
		t.Fatal("expected to find device")
	}
	if !found.LastSeen.Equal(now) {
		t.Errorf("expected LastSeen %v, got %v", now, found.LastSeen)
	}
}

func TestStore_EmptyFile(t *testing.T) {
	// File does not exist
	path := filepath.Join(t.TempDir(), "nonexistent", "devices.json")
	s := NewStore(path)

	devices, err := s.List()
	if err != nil {
		t.Fatalf("List on missing file should not error, got: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected empty slice, got %d devices", len(devices))
	}
}

func TestStore_PersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s1 := NewStore(path)

	d := Device{
		ID:       "dev-1",
		Name:     "iPhone",
		Token:    "tok-aaa",
		PairedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := s1.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify the file exists and is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}
	if _, ok := raw["devices"]; !ok {
		t.Fatal("expected 'devices' key in JSON file")
	}

	// New store instance reads persisted data
	s2 := NewStore(path)
	devices, err := s2.List()
	if err != nil {
		t.Fatalf("List from new store: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device from disk, got %d", len(devices))
	}
	if devices[0].ID != "dev-1" {
		t.Errorf("expected ID dev-1, got %s", devices[0].ID)
	}
	if devices[0].Token != "tok-aaa" {
		t.Errorf("expected Token tok-aaa, got %s", devices[0].Token)
	}
}
