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
		tok := strings.TrimSpace(string(data))
		if tok != "" {
			return tok, nil
		}
	}

	// File missing or empty — generate a new token.
	tok := device.GenerateAdminToken()

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}

	if err := os.WriteFile(path, []byte(tok+"\n"), 0600); err != nil {
		return "", err
	}

	return tok, nil
}
