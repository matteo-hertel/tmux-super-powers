package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// Middleware validates bearer tokens on incoming HTTP requests.
// Open paths (health check, pairing completion) are allowed without authentication.
type Middleware struct {
	adminToken    string
	store         *device.Store
	lastSeenMu    sync.Mutex
	lastSeenCache map[string]time.Time
}

// NewMiddleware creates a new auth middleware.
func NewMiddleware(adminToken string, store *device.Store) *Middleware {
	return &Middleware{
		adminToken:    adminToken,
		store:         store,
		lastSeenCache: make(map[string]time.Time),
	}
}

// Wrap returns an http.Handler that checks authentication before delegating to next.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Open paths — no auth required.
		if isOpenPath(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from header or query param.
		token := extractToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "missing authentication token",
			})
			return
		}

		// Admin token.
		if token == m.adminToken {
			next.ServeHTTP(w, r)
			return
		}

		// Device token.
		if dev := m.store.FindByToken(token); dev != nil {
			m.touchLastSeen(token)
			next.ServeHTTP(w, r)
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid authentication token",
		})
	})
}

// isOpenPath returns true for paths that do not require authentication.
func isOpenPath(r *http.Request) bool {
	// Non-API paths (web UI) are always open — auth happens client-side.
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	switch r.URL.Path {
	case "/api/health":
		return true
	case "/api/pair/complete":
		return true
	}
	return false
}

// extractToken reads the bearer token from the Authorization header,
// falling back to the "token" query parameter (useful for WebSocket handshake).
func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return r.URL.Query().Get("token")
}

// touchLastSeen updates the device's last_seen timestamp, debounced to at most
// once per minute per token to avoid hammering the disk on every request.
func (m *Middleware) touchLastSeen(token string) {
	now := time.Now()

	m.lastSeenMu.Lock()
	last, ok := m.lastSeenCache[token]
	if ok && now.Sub(last) < time.Minute {
		m.lastSeenMu.Unlock()
		return
	}
	m.lastSeenCache[token] = now
	m.lastSeenMu.Unlock()

	m.store.UpdateLastSeen(token, now)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
