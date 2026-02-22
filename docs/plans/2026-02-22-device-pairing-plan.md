# Device Pairing System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a device pairing and auth system to `tsp serve` so only approved devices can access the API.

**Architecture:** File-based device store (`~/.tsp/devices.json`) with bearer token auth middleware. Pairing codes are held in-memory with 5-min TTL. CLI uses an admin token stored at `~/.tsp/admin-token`. Config directory migrates from `~/.tmux-super-powers.yaml` to `~/.tsp/config.yaml`.

**Tech Stack:** Go stdlib (crypto/rand, net/http), `github.com/skip2/go-qrcode`, existing cobra/bubbletea stack.

**Design doc:** `docs/plans/2026-02-22-device-pairing-design.md`

---

### Task 1: Config directory migration (`~/.tsp/`)

**Files:**
- Modify: `config/config.go:48-59` (Load function) and `config/config.go:155-159` (ConfigPath)
- Modify: `config/config_test.go`

**Step 1: Write failing tests for new config path logic**

Add to `config/config_test.go`:

```go
func TestConfigPath_PrefersTspDir(t *testing.T) {
	// ConfigPath should return ~/.tsp/config.yaml
	path := ConfigPath()
	if !strings.HasSuffix(path, filepath.Join(".tsp", "config.yaml")) {
		t.Errorf("ConfigPath() = %q, want suffix .tsp/config.yaml", path)
	}
}

func TestLoad_MigratesOldConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create old-style config
	oldPath := filepath.Join(tmpDir, ".tmux-super-powers.yaml")
	content := []byte("directories:\n  - /tmp/migrated\neditor: nano\n")
	if err := os.WriteFile(oldPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Load with custom home dir
	cfg, newPath, err := LoadWithMigration(tmpDir)
	if err != nil {
		t.Fatalf("LoadWithMigration() error = %v", err)
	}

	// Should have migrated
	expectedNew := filepath.Join(tmpDir, ".tsp", "config.yaml")
	if newPath != expectedNew {
		t.Errorf("new path = %q, want %q", newPath, expectedNew)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != "/tmp/migrated" {
		t.Errorf("config not migrated correctly: %+v", cfg)
	}

	// New file should exist
	if _, err := os.Stat(expectedNew); os.IsNotExist(err) {
		t.Error("new config file was not created")
	}
}

func TestLoad_NewPathTakesPriority(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both old and new config
	oldPath := filepath.Join(tmpDir, ".tmux-super-powers.yaml")
	os.WriteFile(oldPath, []byte("editor: old\n"), 0644)

	newDir := filepath.Join(tmpDir, ".tsp")
	os.MkdirAll(newDir, 0755)
	newPath := filepath.Join(newDir, "config.yaml")
	os.WriteFile(newPath, []byte("editor: new\n"), 0644)

	cfg, _, err := LoadWithMigration(tmpDir)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if cfg.Editor != "new" {
		t.Errorf("Editor = %q, want \"new\" (new path should take priority)", cfg.Editor)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./config/ -run "TestConfigPath_PrefersTspDir|TestLoad_MigratesOldConfig|TestLoad_NewPathTakesPriority" -v`
Expected: FAIL — `LoadWithMigration` doesn't exist, `ConfigPath` returns old path.

**Step 3: Implement config migration**

Update `config/config.go`:

1. Add `TspDir()` function returning `~/.tsp`.
2. Change `ConfigPath()` to return `~/.tsp/config.yaml`.
3. Add `OldConfigPath()` returning `~/.tmux-super-powers.yaml`.
4. Add `LoadWithMigration(homeDir string) (*Config, string, error)` that:
   - Checks `<homeDir>/.tsp/config.yaml` first
   - If missing, checks `<homeDir>/.tmux-super-powers.yaml`
   - If old exists, copies to new location (creating `~/.tsp/` dir)
   - Prints migration message to stderr
5. Update `Load()` to call `LoadWithMigration` with the real home dir.
6. Update `Save()` to write to the new path, ensuring `~/.tsp/` exists.

**Step 4: Run tests to verify they pass**

Run: `go test ./config/ -v`
Expected: All PASS

**Step 5: Run full test suite to check nothing broke**

Run: `go test ./...`
Expected: All PASS

**Step 6: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat: migrate config to ~/.tsp/ directory"
```

---

### Task 2: Device store (`internal/device/store.go`)

**Files:**
- Create: `internal/device/store.go`
- Create: `internal/device/store_test.go`

**Step 1: Write failing tests for the device store**

Create `internal/device/store_test.go`:

```go
package device

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_AddAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := NewStore(path)

	d := Device{
		ID:       "d_abc123",
		Name:     "Test Phone",
		Token:    "tsp_deadbeef",
		PairedAt: time.Now().UTC(),
	}
	if err := store.Add(d); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	devices, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("List() returned %d devices, want 1", len(devices))
	}
	if devices[0].ID != "d_abc123" {
		t.Errorf("device ID = %q, want %q", devices[0].ID, "d_abc123")
	}
}

func TestStore_FindByToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := NewStore(path)

	store.Add(Device{ID: "d_1", Name: "Phone", Token: "tsp_aaa"})
	store.Add(Device{ID: "d_2", Name: "Laptop", Token: "tsp_bbb"})

	d := store.FindByToken("tsp_bbb")
	if d == nil {
		t.Fatal("FindByToken() returned nil")
	}
	if d.ID != "d_2" {
		t.Errorf("found ID = %q, want %q", d.ID, "d_2")
	}

	if store.FindByToken("tsp_missing") != nil {
		t.Error("FindByToken() should return nil for unknown token")
	}
}

func TestStore_Remove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := NewStore(path)

	store.Add(Device{ID: "d_1", Name: "Phone", Token: "tsp_aaa"})
	store.Add(Device{ID: "d_2", Name: "Laptop", Token: "tsp_bbb"})

	if err := store.Remove("d_1"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	devices, _ := store.List()
	if len(devices) != 1 {
		t.Fatalf("expected 1 device after remove, got %d", len(devices))
	}
	if devices[0].ID != "d_2" {
		t.Errorf("remaining device ID = %q, want %q", devices[0].ID, "d_2")
	}
}

func TestStore_UpdateLastSeen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := NewStore(path)

	store.Add(Device{ID: "d_1", Name: "Phone", Token: "tsp_aaa"})

	now := time.Now().UTC()
	store.UpdateLastSeen("tsp_aaa", now)

	devices, _ := store.List()
	if devices[0].LastSeen.IsZero() {
		t.Error("LastSeen should be set after UpdateLastSeen()")
	}
}

func TestStore_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := NewStore(path)

	devices, err := store.List()
	if err != nil {
		t.Fatalf("List() on missing file should not error, got %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(devices))
	}
}

func TestStore_PersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store1 := NewStore(path)
	store1.Add(Device{ID: "d_1", Name: "Phone", Token: "tsp_aaa"})

	// New store instance reads from same file
	store2 := NewStore(path)
	devices, _ := store2.List()
	if len(devices) != 1 {
		t.Fatalf("new store should see persisted devices, got %d", len(devices))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/device/ -v`
Expected: FAIL — package doesn't exist.

**Step 3: Implement the device store**

Create `internal/device/store.go`:

```go
package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Device represents a paired device.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Token    string    `json:"token"`
	PairedAt time.Time `json:"paired_at"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

type deviceFile struct {
	Devices []Device `json:"devices"`
}

// Store manages paired devices in a JSON file.
type Store struct {
	path string
}

// NewStore creates a store backed by the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// List returns all paired devices.
func (s *Store) List() ([]Device, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f deviceFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return f.Devices, nil
}

// Add appends a device and persists to disk.
func (s *Store) Add(d Device) error {
	devices, err := s.List()
	if err != nil {
		return err
	}
	devices = append(devices, d)
	return s.write(devices)
}

// Remove deletes a device by ID and persists.
func (s *Store) Remove(id string) error {
	devices, err := s.List()
	if err != nil {
		return err
	}
	var filtered []Device
	for _, d := range devices {
		if d.ID != id {
			filtered = append(filtered, d)
		}
	}
	return s.write(filtered)
}

// FindByToken returns the device with the given token, or nil.
func (s *Store) FindByToken(token string) *Device {
	devices, err := s.List()
	if err != nil {
		return nil
	}
	for i := range devices {
		if devices[i].Token == token {
			return &devices[i]
		}
	}
	return nil
}

// UpdateLastSeen sets the last_seen timestamp for a device by token.
func (s *Store) UpdateLastSeen(token string, t time.Time) {
	devices, err := s.List()
	if err != nil {
		return
	}
	for i := range devices {
		if devices[i].Token == token {
			devices[i].LastSeen = t
			s.write(devices)
			return
		}
	}
}

func (s *Store) write(devices []Device) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(deviceFile{Devices: devices}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/device/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/device/store.go internal/device/store_test.go
git commit -m "feat: add device store for paired devices"
```

---

### Task 3: Token generation (`internal/device/token.go`)

**Files:**
- Create: `internal/device/token.go`
- Create: `internal/device/token_test.go`

**Step 1: Write failing tests**

Create `internal/device/token_test.go`:

```go
package device

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token := GenerateToken()
	if !strings.HasPrefix(token, "tsp_") {
		t.Errorf("token %q should start with tsp_", token)
	}
	// tsp_ (4) + 32 hex chars = 36 total
	if len(token) != 36 {
		t.Errorf("token length = %d, want 36", len(token))
	}
}

func TestGenerateToken_Unique(t *testing.T) {
	t1 := GenerateToken()
	t2 := GenerateToken()
	if t1 == t2 {
		t.Error("two generated tokens should not be identical")
	}
}

func TestGenerateDeviceID(t *testing.T) {
	id := GenerateDeviceID()
	if !strings.HasPrefix(id, "d_") {
		t.Errorf("device ID %q should start with d_", id)
	}
	if len(id) != 14 { // d_ (2) + 12 hex chars
		t.Errorf("device ID length = %d, want 14", len(id))
	}
}

func TestGenerateAdminToken(t *testing.T) {
	token := GenerateAdminToken()
	if !strings.HasPrefix(token, "tsp_admin_") {
		t.Errorf("admin token %q should start with tsp_admin_", token)
	}
}

func TestGeneratePairingCode(t *testing.T) {
	code := GeneratePairingCode()
	if len(code) != 6 {
		t.Errorf("pairing code length = %d, want 6", len(code))
	}
	// Should be alphanumeric uppercase
	for _, c := range code {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Errorf("pairing code contains invalid char: %c", c)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/device/ -run "TestGenerate" -v`
Expected: FAIL

**Step 3: Implement token generation**

Create `internal/device/token.go`:

```go
package device

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

const pairingCodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I to avoid confusion

// GenerateToken creates a bearer token: tsp_ + 32 hex chars.
func GenerateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "tsp_" + hex.EncodeToString(b)
}

// GenerateDeviceID creates a device ID: d_ + 12 hex chars.
func GenerateDeviceID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "d_" + hex.EncodeToString(b)
}

// GenerateAdminToken creates an admin token: tsp_admin_ + 32 hex chars.
func GenerateAdminToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "tsp_admin_" + hex.EncodeToString(b)
}

// GeneratePairingCode creates a 6-character alphanumeric code (uppercase, no ambiguous chars).
func GeneratePairingCode() string {
	code := make([]byte, 6)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(pairingCodeChars))))
		code[i] = pairingCodeChars[n.Int64()]
	}
	return string(code)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/device/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/device/token.go internal/device/token_test.go
git commit -m "feat: add token and pairing code generation"
```

---

### Task 4: Pairing state manager (`internal/device/pairing.go`)

**Files:**
- Create: `internal/device/pairing.go`
- Create: `internal/device/pairing_test.go`

**Step 1: Write failing tests**

Create `internal/device/pairing_test.go`:

```go
package device

import (
	"testing"
	"time"
)

func TestPairingManager_Initiate(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)
	code, err := pm.Initiate("Test Phone")
	if err != nil {
		t.Fatalf("Initiate() error = %v", err)
	}
	if len(code) != 6 {
		t.Errorf("code length = %d, want 6", len(code))
	}
}

func TestPairingManager_Complete(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)
	code, _ := pm.Initiate("Test Phone")

	name, err := pm.Complete(code)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if name != "Test Phone" {
		t.Errorf("name = %q, want %q", name, "Test Phone")
	}
}

func TestPairingManager_CompleteInvalidCode(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)

	_, err := pm.Complete("ZZZZZZ")
	if err == nil {
		t.Error("Complete() should fail for invalid code")
	}
}

func TestPairingManager_CompleteExpired(t *testing.T) {
	pm := NewPairingManager(1 * time.Millisecond)
	code, _ := pm.Initiate("Test Phone")

	time.Sleep(5 * time.Millisecond)

	_, err := pm.Complete(code)
	if err == nil {
		t.Error("Complete() should fail for expired code")
	}
}

func TestPairingManager_CompleteAlreadyUsed(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)
	code, _ := pm.Initiate("Test Phone")

	pm.Complete(code) // first use

	_, err := pm.Complete(code)
	if err == nil {
		t.Error("Complete() should fail for already-used code")
	}
}

func TestPairingManager_NewInitiateInvalidatesPrevious(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)
	code1, _ := pm.Initiate("Phone 1")
	pm.Initiate("Phone 2")

	_, err := pm.Complete(code1)
	if err == nil {
		t.Error("first code should be invalidated by second Initiate()")
	}
}

func TestPairingManager_Status(t *testing.T) {
	pm := NewPairingManager(5 * time.Minute)
	code, _ := pm.Initiate("Test Phone")

	claimed, name := pm.Status(code)
	if claimed {
		t.Error("should not be claimed yet")
	}

	pm.Complete(code)

	claimed, name = pm.Status(code)
	if !claimed {
		t.Error("should be claimed after Complete()")
	}
	if name != "Test Phone" {
		t.Errorf("name = %q, want %q", name, "Test Phone")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/device/ -run "TestPairingManager" -v`
Expected: FAIL

**Step 3: Implement pairing manager**

Create `internal/device/pairing.go`:

```go
package device

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidCode = errors.New("invalid pairing code")
	ErrCodeExpired = errors.New("pairing code expired")
	ErrCodeUsed    = errors.New("pairing code already used")
)

type pendingPair struct {
	code       string
	deviceName string
	expiresAt  time.Time
	claimed    bool
}

// PairingManager handles in-memory pairing code lifecycle.
type PairingManager struct {
	mu      sync.Mutex
	pending *pendingPair
	ttl     time.Duration
}

// NewPairingManager creates a pairing manager with the given code TTL.
func NewPairingManager(ttl time.Duration) *PairingManager {
	return &PairingManager{ttl: ttl}
}

// Initiate generates a new pairing code, invalidating any previous one.
func (pm *PairingManager) Initiate(deviceName string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	code := GeneratePairingCode()
	pm.pending = &pendingPair{
		code:       code,
		deviceName: deviceName,
		expiresAt:  time.Now().Add(pm.ttl),
	}
	return code, nil
}

// Complete validates a pairing code and marks it as claimed.
// Returns the device name that was set during Initiate.
func (pm *PairingManager) Complete(code string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.pending == nil || pm.pending.code != code {
		return "", ErrInvalidCode
	}
	if time.Now().After(pm.pending.expiresAt) {
		return "", ErrCodeExpired
	}
	if pm.pending.claimed {
		return "", ErrCodeUsed
	}

	pm.pending.claimed = true
	return pm.pending.deviceName, nil
}

// Status returns whether a code has been claimed and the device name.
func (pm *PairingManager) Status(code string) (claimed bool, deviceName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.pending == nil || pm.pending.code != code {
		return false, ""
	}
	return pm.pending.claimed, pm.pending.deviceName
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/device/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/device/pairing.go internal/device/pairing_test.go
git commit -m "feat: add pairing code manager with TTL"
```

---

### Task 5: Admin token management (`internal/auth/admin.go`)

**Files:**
- Create: `internal/auth/admin.go`
- Create: `internal/auth/admin_test.go`

**Step 1: Write failing tests**

Create `internal/auth/admin_test.go`:

```go
package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateAdminToken_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin-token")

	token, err := LoadOrCreateAdminToken(path)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !strings.HasPrefix(token, "tsp_admin_") {
		t.Errorf("token %q should start with tsp_admin_", token)
	}

	// File should exist
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("token file not created: %v", err)
	}
	if string(data) != token {
		t.Errorf("file content = %q, want %q", string(data), token)
	}
}

func TestLoadOrCreateAdminToken_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin-token")

	os.WriteFile(path, []byte("tsp_admin_existing"), 0600)

	token, err := LoadOrCreateAdminToken(path)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if token != "tsp_admin_existing" {
		t.Errorf("token = %q, want %q", token, "tsp_admin_existing")
	}
}

func TestLoadOrCreateAdminToken_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin-token")

	LoadOrCreateAdminToken(path)

	info, _ := os.Stat(path)
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/ -v`
Expected: FAIL — package doesn't exist.

**Step 3: Implement admin token management**

Create `internal/auth/admin.go`:

```go
package auth

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// LoadOrCreateAdminToken reads the admin token from disk, or generates one if missing.
func LoadOrCreateAdminToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if token != "" {
			return token, nil
		}
	}

	token := device.GenerateAdminToken()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		return "", err
	}
	return token, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/auth/admin.go internal/auth/admin_test.go
git commit -m "feat: add admin token management"
```

---

### Task 6: Auth middleware (`internal/auth/middleware.go`)

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

**Step 1: Write failing tests**

Create `internal/auth/middleware_test.go`:

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

func TestMiddleware_AllowsHealth(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint should be allowed without auth, got %d", w.Code)
	}
}

func TestMiddleware_AllowsPairComplete(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/pair/complete", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("pair/complete should be allowed without auth, got %d", w.Code)
	}
}

func TestMiddleware_RejectsNoToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_AcceptsAdminToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer tsp_admin_test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("admin token should be accepted, got %d", w.Code)
	}
}

func TestMiddleware_AcceptsDeviceToken(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "devices.json")
	store := device.NewStore(storePath)
	store.Add(device.Device{
		ID: "d_1", Name: "Phone", Token: "tsp_device123",
		PairedAt: time.Now().UTC(),
	})
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer tsp_device123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("device token should be accepted, got %d", w.Code)
	}
}

func TestMiddleware_AcceptsTokenQueryParam(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "devices.json")
	store := device.NewStore(storePath)
	store.Add(device.Device{
		ID: "d_1", Name: "Phone", Token: "tsp_device123",
		PairedAt: time.Now().UTC(),
	})
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/ws?token=tsp_device123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("token query param should be accepted, got %d", w.Code)
	}
}

func TestMiddleware_RejectsInvalidToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	m := NewMiddleware("tsp_admin_test", store)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer tsp_wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/ -v`
Expected: FAIL

**Step 3: Implement auth middleware**

Create `internal/auth/middleware.go`:

```go
package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// openPaths are routes that don't require authentication.
var openPaths = []string{
	"/api/health",
	"/api/pair/complete",
}

// Middleware validates bearer tokens on API requests.
type Middleware struct {
	adminToken    string
	store         *device.Store
	lastSeenMu    sync.Mutex
	lastSeenCache map[string]time.Time // token -> last update time (debounce)
}

// NewMiddleware creates auth middleware.
func NewMiddleware(adminToken string, store *device.Store) *Middleware {
	return &Middleware{
		adminToken:    adminToken,
		store:         store,
		lastSeenCache: make(map[string]time.Time),
	}
}

// Wrap returns an http.Handler that checks auth before delegating.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.isOpenPath(r) {
			next.ServeHTTP(w, r)
			return
		}

		token := m.extractToken(r)
		if token == "" {
			writeAuthError(w, "missing authentication token")
			return
		}

		if token == m.adminToken {
			next.ServeHTTP(w, r)
			return
		}

		if d := m.store.FindByToken(token); d != nil {
			m.maybeUpdateLastSeen(token)
			next.ServeHTTP(w, r)
			return
		}

		writeAuthError(w, "invalid authentication token")
	})
}

func (m *Middleware) isOpenPath(r *http.Request) bool {
	for _, p := range openPaths {
		if r.URL.Path == p {
			return true
		}
	}
	return false
}

func (m *Middleware) extractToken(r *http.Request) string {
	// Check Authorization header first
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fall back to query param (for WebSocket)
	return r.URL.Query().Get("token")
}

func (m *Middleware) maybeUpdateLastSeen(token string) {
	m.lastSeenMu.Lock()
	defer m.lastSeenMu.Unlock()

	now := time.Now().UTC()
	if last, ok := m.lastSeenCache[token]; ok && now.Sub(last) < time.Minute {
		return // debounce: only update once per minute
	}
	m.lastSeenCache[token] = now
	m.store.UpdateLastSeen(token, now)
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "feat: add auth middleware with bearer token validation"
```

---

### Task 7: Wire auth into the server

**Files:**
- Modify: `internal/server/server.go:23-62` (Server struct, New, Start, registerRoutes)
- Modify: `internal/server/handlers.go` (add pairing handlers)
- Modify: `internal/server/handlers_test.go` (update test helper)

**Step 1: Write failing tests for pairing endpoints**

Add to `internal/server/handlers_test.go`:

```go
func TestPairInitiate_RequiresAdminAuth(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	handler := srv.authMiddleware.Wrap(mux)

	req := httptest.NewRequest("POST", "/api/pair/initiate", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

func TestPairComplete_Open(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	handler := srv.authMiddleware.Wrap(mux)

	// First initiate a pairing (with admin token)
	body := strings.NewReader(`{"name":"Test Phone"}`)
	req := httptest.NewRequest("POST", "/api/pair/initiate", body)
	req.Header.Set("Authorization", "Bearer "+srv.adminToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("initiate failed: %d %s", w.Code, w.Body.String())
	}

	var initResp struct {
		Code string `json:"code"`
	}
	json.NewDecoder(w.Body).Decode(&initResp)

	// Complete without auth (should be open)
	body = strings.NewReader(fmt.Sprintf(`{"code":"%s","name":"Test Phone"}`, initResp.Code))
	req = httptest.NewRequest("POST", "/api/pair/complete", body)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("pair/complete should work without auth, got %d %s", w.Code, w.Body.String())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run "TestPair" -v`
Expected: FAIL — `authMiddleware` and `adminToken` fields don't exist on Server.

**Step 3: Update server to wire in auth**

Modify `internal/server/server.go`:

1. Add imports for `internal/auth` and `internal/device`.
2. Add fields to `Server` struct: `deviceStore *device.Store`, `pairing *device.PairingManager`, `adminToken string`, `authMiddleware *auth.Middleware`.
3. Update `New()` to accept a `tspDir string` parameter:
   - Create device store pointing at `<tspDir>/devices.json`
   - Load/create admin token from `<tspDir>/admin-token`
   - Create pairing manager with 5-min TTL
   - Create auth middleware with admin token + device store
4. Update `Start()` to wrap the mux: `withLogging(withCORS(s.authMiddleware.Wrap(mux)))`.
5. Add pairing routes to `registerRoutes()`:
   ```go
   mux.HandleFunc("POST /api/pair/initiate", s.handlePairInitiate)
   mux.HandleFunc("POST /api/pair/complete", s.handlePairComplete)
   mux.HandleFunc("GET /api/pair/status", s.handlePairStatus)
   ```

Add pairing handlers to `internal/server/handlers.go`:

```go
func (s *Server) handlePairInitiate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Name == "" {
		req.Name = "unnamed device"
	}

	code, err := s.pairing.Initiate(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"code":    code,
		"address": r.Host,
	})
}

func (s *Server) handlePairComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	deviceName, err := s.pairing.Complete(req.Code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Use the name from the client if provided, otherwise use the one from initiate
	name := req.Name
	if name == "" {
		name = deviceName
	}

	token := device.GenerateToken()
	id := device.GenerateDeviceID()
	d := device.Device{
		ID:       id,
		Name:     name,
		Token:    token,
		PairedAt: time.Now().UTC(),
	}
	if err := s.deviceStore.Add(d); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save device")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":     token,
		"device_id": id,
	})
}

func (s *Server) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "code query param required")
		return
	}
	claimed, name := s.pairing.Status(code)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"claimed":     claimed,
		"device_name": name,
	})
}
```

Update `newTestServer()` in `handlers_test.go` to create a temp device store and auth middleware.

**Step 4: Update `internal/cmd/serve.go` to pass tspDir to `server.New()`**

Change `srv := server.New(cfg)` to `srv := server.New(cfg, config.TspDir())`.

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: All PASS

**Step 6: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/server/server.go internal/server/handlers.go internal/server/handlers_test.go internal/cmd/serve.go
git commit -m "feat: wire auth middleware and pairing endpoints into server"
```

---

### Task 8: Add QR code dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run: `go get github.com/skip2/go-qrcode`

**Step 2: Tidy**

Run: `go mod tidy`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add go-qrcode for terminal QR rendering"
```

---

### Task 9: CLI `tsp device` commands (`internal/cmd/device.go`)

**Files:**
- Create: `internal/cmd/device.go`
- Modify: `internal/cmd/root.go:29` (add `rootCmd.AddCommand(deviceCmd)`)

**Step 1: Implement the device command and subcommands**

Create `internal/cmd/device.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/device"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage paired devices",
	Long:  "Add, list, and revoke devices that can access the tsp API server.",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var devicePairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Generate a pairing code for a new device",
	Run: func(cmd *cobra.Command, args []string) {
		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			name = "unnamed device"
		}

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		// Read admin token
		adminTokenPath := config.TspDir() + "/admin-token"
		tokenData, err := os.ReadFile(adminTokenPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading admin token (is the server running?): %v\n", err)
			os.Exit(1)
		}
		adminToken := strings.TrimSpace(string(tokenData))

		// Determine server address
		port := cfg.Serve.Port
		if port == 0 {
			port = 7777
		}
		// Try localhost first since CLI runs on same machine
		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

		// Initiate pairing
		body := strings.NewReader(fmt.Sprintf(`{"name":"%s"}`, name))
		req, _ := http.NewRequest("POST", baseURL+"/api/pair/initiate", body)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			fmt.Fprintf(os.Stderr, "Server error: %s\n", respBody)
			os.Exit(1)
		}

		var result struct {
			Code    string `json:"code"`
			Address string `json:"address"`
		}
		json.NewDecoder(resp.Body).Decode(&result)

		// Build QR URL using the server's external address
		address := result.Address
		if address == "" {
			address = fmt.Sprintf("127.0.0.1:%d", port)
		}
		qrURL := fmt.Sprintf("http://%s/api/pair/complete?code=%s", address, result.Code)

		// Render QR to terminal
		qr, err := qrcode.New(qrURL, qrcode.Medium)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating QR: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(qr.ToSmallString(false))
		fmt.Printf("Pairing code: %s\n", result.Code)
		fmt.Printf("Expires in 5 minutes\n\n")
		fmt.Printf("Mobile: Scan the QR code above\n")
		fmt.Printf("Web:    Paste the code: %s\n\n", result.Code)

		// Poll for completion
		fmt.Print("Waiting for device to pair...")
		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)

			statusReq, _ := http.NewRequest("GET",
				fmt.Sprintf("%s/api/pair/status?code=%s", baseURL, result.Code), nil)
			statusReq.Header.Set("Authorization", "Bearer "+adminToken)
			statusResp, err := http.DefaultClient.Do(statusReq)
			if err != nil {
				continue
			}

			var status struct {
				Claimed    bool   `json:"claimed"`
				DeviceName string `json:"device_name"`
			}
			json.NewDecoder(statusResp.Body).Decode(&status)
			statusResp.Body.Close()

			if status.Claimed {
				fmt.Printf("\nDevice paired: %s\n", status.DeviceName)
				return
			}
			fmt.Print(".")
		}
		fmt.Println("\nPairing code expired.")
	},
}

var deviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all paired devices",
	Run: func(cmd *cobra.Command, args []string) {
		store := device.NewStore(config.TspDir() + "/devices.json")
		devices, err := store.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading devices: %v\n", err)
			os.Exit(1)
		}

		if len(devices) == 0 {
			fmt.Println("No paired devices. Use 'tsp device pair' to add one.")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPAIRED AT\tLAST SEEN")
		for _, d := range devices {
			lastSeen := "never"
			if !d.LastSeen.IsZero() {
				lastSeen = d.LastSeen.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				d.ID, d.Name,
				d.PairedAt.Format("2006-01-02 15:04"),
				lastSeen,
			)
		}
		w.Flush()
	},
}

var deviceRevokeCmd = &cobra.Command{
	Use:   "revoke [device-id or name]",
	Short: "Revoke a paired device",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]
		store := device.NewStore(config.TspDir() + "/devices.json")
		devices, err := store.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading devices: %v\n", err)
			os.Exit(1)
		}

		// Find by ID first, then by name
		var toRemove *device.Device
		for i := range devices {
			if devices[i].ID == target {
				toRemove = &devices[i]
				break
			}
		}
		if toRemove == nil {
			for i := range devices {
				if strings.EqualFold(devices[i].Name, target) {
					toRemove = &devices[i]
					break
				}
			}
		}

		if toRemove == nil {
			fmt.Fprintf(os.Stderr, "Device not found: %s\n", target)
			os.Exit(1)
		}

		if err := store.Remove(toRemove.ID); err != nil {
			fmt.Fprintf(os.Stderr, "Error revoking device: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Revoked device: %s (%s)\n", toRemove.Name, toRemove.ID)
	},
}

func init() {
	devicePairCmd.Flags().String("name", "", "Name for the device being paired")
	deviceCmd.AddCommand(devicePairCmd)
	deviceCmd.AddCommand(deviceListCmd)
	deviceCmd.AddCommand(deviceRevokeCmd)
}
```

**Step 2: Register in root command**

Add `rootCmd.AddCommand(deviceCmd)` in `internal/cmd/root.go` init().

**Step 3: Build to verify compilation**

Run: `go build ./cmd/tsp`
Expected: Builds successfully.

**Step 4: Commit**

```bash
git add internal/cmd/device.go internal/cmd/root.go
git commit -m "feat: add tsp device pair/list/revoke commands"
```

---

### Task 10: Integration test — full pairing flow

**Files:**
- Create: `internal/server/pairing_test.go`

**Step 1: Write the integration test**

Create `internal/server/pairing_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPairingFlow_EndToEnd(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	handler := srv.authMiddleware.Wrap(mux)

	// 1. Protected endpoint should reject without auth
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}

	// 2. Initiate pairing (admin auth)
	body := strings.NewReader(`{"name":"Integration Test Phone"}`)
	req = httptest.NewRequest("POST", "/api/pair/initiate", body)
	req.Header.Set("Authorization", "Bearer "+srv.adminToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initiate: expected 200, got %d %s", w.Code, w.Body.String())
	}

	var initResp struct {
		Code string `json:"code"`
	}
	json.NewDecoder(w.Body).Decode(&initResp)
	if initResp.Code == "" {
		t.Fatal("initiate returned empty code")
	}

	// 3. Complete pairing (no auth required)
	body = strings.NewReader(`{"code":"` + initResp.Code + `","name":"Integration Test Phone"}`)
	req = httptest.NewRequest("POST", "/api/pair/complete", body)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d %s", w.Code, w.Body.String())
	}

	var completeResp struct {
		Token    string `json:"token"`
		DeviceID string `json:"device_id"`
	}
	json.NewDecoder(w.Body).Decode(&completeResp)
	if completeResp.Token == "" {
		t.Fatal("complete returned empty token")
	}

	// 4. Use the device token to access protected endpoint
	req = httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+completeResp.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	// health is open, but let's test a protected endpoint
	req = httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+completeResp.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("device token should grant access, got %d", w.Code)
	}

	// 5. Same code should not work again
	body = strings.NewReader(`{"code":"` + initResp.Code + `","name":"Attacker"}`)
	req = httptest.NewRequest("POST", "/api/pair/complete", body)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("reused code should be rejected, got %d", w.Code)
	}
}
```

**Step 2: Run the integration test**

Run: `go test ./internal/server/ -run "TestPairingFlow" -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/server/pairing_test.go
git commit -m "test: add end-to-end pairing flow integration test"
```

---

### Task 11: Build and smoke test

**Step 1: Build**

Run: `go build -o tsp ./cmd/tsp`
Expected: Builds successfully.

**Step 2: Verify help output**

Run: `./tsp device --help`
Expected: Shows `pair`, `list`, `revoke` subcommands.

Run: `./tsp device list`
Expected: "No paired devices" message.

**Step 3: Commit any final fixes**

If anything needs fixing, address and commit.

**Step 4: Final full test run**

Run: `go test ./... -v`
Expected: All PASS
