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

// pendingPair holds the state of a single in-flight pairing attempt.
type pendingPair struct {
	code       string
	deviceName string
	expiresAt  time.Time
	claimed    bool
}

// PairingManager holds at most one pending pairing attempt in memory.
// It is safe for concurrent use.
type PairingManager struct {
	mu      sync.Mutex
	ttl     time.Duration
	pending *pendingPair
}

// NewPairingManager creates a PairingManager whose codes expire after ttl.
func NewPairingManager(ttl time.Duration) *PairingManager {
	return &PairingManager{ttl: ttl}
}

// Initiate generates a new pairing code for the given device name.
// Any previously pending (unclaimed) pair is invalidated.
func (pm *PairingManager) Initiate(deviceName string) (string, error) {
	code := GeneratePairingCode()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.pending = &pendingPair{
		code:       code,
		deviceName: deviceName,
		expiresAt:  time.Now().Add(pm.ttl),
		claimed:    false,
	}

	return code, nil
}

// Complete validates the code, marks it as claimed, and returns the device name.
// Returns ErrInvalidCode if the code does not match, ErrCodeExpired if it has
// passed its TTL, or ErrCodeUsed if it was already claimed.
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

// Status reports whether the given code has been claimed and the associated
// device name. For unknown codes it returns (false, "").
func (pm *PairingManager) Status(code string) (claimed bool, deviceName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.pending == nil || pm.pending.code != code {
		return false, ""
	}

	return pm.pending.claimed, pm.pending.deviceName
}
