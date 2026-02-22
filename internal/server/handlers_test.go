package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/auth"
	"github.com/matteo-hertel/tmux-super-powers/internal/device"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

// newTestServer creates a Server with a properly initialised monitor for testing.
func newTestServer() *Server {
	tmpDir, _ := os.MkdirTemp("", "tsp-test-*")
	deviceStore := device.NewStore(filepath.Join(tmpDir, "devices.json"))
	adminToken := "tsp_admin_testtoken"
	authMw := auth.NewMiddleware(adminToken, deviceStore)

	return &Server{
		cfg:     &config.Config{},
		monitor: service.NewMonitor(500, nil, ""),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		deviceStore:    deviceStore,
		pairing:        device.NewPairingManager(5 * time.Minute),
		adminToken:     adminToken,
		authMiddleware: authMw,
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", srv.handleHealth)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 200 or 503, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestConfigEndpoint(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", srv.handleConfig)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Should return 200 even with nil config (marshals as null)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestSessionsEndpointWithMonitor(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", srv.handleListSessions)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Should return 503 if tmux is not running, or 200 with session list if it is
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 200 or 503, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}
