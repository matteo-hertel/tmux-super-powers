package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateAdminToken_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "admin_token")

	tok, err := LoadOrCreateAdminToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(tok, "tsp_admin_") {
		t.Errorf("expected prefix tsp_admin_, got %s", tok)
	}

	// File must exist after the call.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected token file to exist after creation")
	}

	// Read file and verify content matches returned token.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read token file: %v", err)
	}
	if strings.TrimSpace(string(data)) != tok {
		t.Errorf("file content %q does not match returned token %q", string(data), tok)
	}
}

func TestLoadOrCreateAdminToken_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin_token")

	existing := "tsp_admin_existing"
	if err := os.WriteFile(path, []byte(existing+"\n"), 0600); err != nil {
		t.Fatalf("failed to write seed file: %v", err)
	}

	tok, err := LoadOrCreateAdminToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tok != existing {
		t.Errorf("expected %q, got %q", existing, tok)
	}
}

func TestLoadOrCreateAdminToken_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin_token")

	_, err := LoadOrCreateAdminToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat token file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected file permissions 0600, got %04o", perm)
	}
}
