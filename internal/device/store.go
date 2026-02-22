package device

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Device represents a paired mobile device.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Token    string    `json:"token"`
	PairedAt time.Time `json:"paired_at"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

// storeFile is the on-disk JSON format.
type storeFile struct {
	Devices []Device `json:"devices"`
}

// Store manages paired devices backed by a JSON file.
type Store struct {
	path    string
	mu      sync.Mutex
	devices []Device
}

// NewStore creates a store backed by the given JSON file path.
// The file does not need to exist yet; it will be created on the first write.
func NewStore(path string) *Store {
	s := &Store{path: path}
	s.load()
	return s
}

// List returns all paired devices. If the backing file is missing or empty,
// it returns an empty slice and no error.
// The store re-reads from disk so that external changes (e.g. tsp device revoke)
// are always reflected.
func (s *Store) List() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loadLocked()
	out := make([]Device, len(s.devices))
	copy(out, s.devices)
	return out, nil
}

// Add appends a device and persists to disk.
func (s *Store) Add(d Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.devices = append(s.devices, d)
	return s.write()
}

// Remove deletes a device by ID and persists to disk.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]Device, 0, len(s.devices))
	for _, d := range s.devices {
		if d.ID != id {
			filtered = append(filtered, d)
		}
	}
	s.devices = filtered
	return s.write()
}

// FindByToken looks up a device by its auth token.
// Returns nil if no device matches.
// Re-reads from disk so revoked devices are rejected immediately.
func (s *Store) FindByToken(token string) *Device {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loadLocked()
	for i := range s.devices {
		if s.devices[i].Token == token {
			d := s.devices[i]
			return &d
		}
	}
	return nil
}

// UpdateLastSeen sets the last_seen timestamp for the device matching the
// given token and persists to disk. If the token is not found, this is a no-op.
func (s *Store) UpdateLastSeen(token string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.devices {
		if s.devices[i].Token == token {
			s.devices[i].LastSeen = t
			_ = s.write()
			return
		}
	}
}

// load reads the JSON file into memory (acquires lock).
func (s *Store) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
}

// loadLocked reads the JSON file into memory. Caller must hold s.mu.
// If the file does not exist, the device list is left empty. Any other
// read/parse error is silently ignored so that a corrupted file does not
// prevent the store from being used.
func (s *Store) loadLocked() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		// File missing or unreadable — start empty.
		s.devices = nil
		return
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return
	}
	s.devices = sf.Devices
}

// write persists the current device list to the JSON file.
func (s *Store) write() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Join(errors.New("device store: create directory"), err)
	}

	sf := storeFile{Devices: s.devices}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return errors.Join(errors.New("device store: marshal"), err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return errors.Join(errors.New("device store: write file"), err)
	}
	return nil
}
