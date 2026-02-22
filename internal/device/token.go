package device

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

const pairingCodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I to avoid confusion

// GenerateToken creates a bearer token: tsp_ + 32 hex chars.
func GenerateToken() string {
	return "tsp_" + randomHex(16)
}

// GenerateDeviceID creates a device ID: d_ + 12 hex chars.
func GenerateDeviceID() string {
	return "d_" + randomHex(6)
}

// GenerateAdminToken creates an admin token: tsp_admin_ + 32 hex chars.
func GenerateAdminToken() string {
	return "tsp_admin_" + randomHex(16)
}

// GeneratePairingCode creates a 6-character alphanumeric code (uppercase, no ambiguous chars).
func GeneratePairingCode() string {
	code := make([]byte, 6)
	max := big.NewInt(int64(len(pairingCodeChars)))
	for i := range code {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("crypto/rand failed: " + err.Error())
		}
		code[i] = pairingCodeChars[n.Int64()]
	}
	return string(code)
}

// randomHex returns n random bytes encoded as 2*n hex characters.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
