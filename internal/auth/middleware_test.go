package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// dummyHandler returns 200 with body "ok" — used as the wrapped handler.
var dummyHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
})

func TestMiddleware_AllowsHealth(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_AllowsPairComplete(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodPost, "/api/pair/complete", nil)
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_RejectsNoToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "missing authentication token" {
		t.Errorf("expected error 'missing authentication token', got %q", body["error"])
	}
}

func TestMiddleware_AcceptsAdminToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_AcceptsDeviceToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	devToken := "tsp_device123"
	if err := store.Add(device.Device{
		ID:       "d_001",
		Name:     "iPhone",
		Token:    devToken,
		PairedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to add device: %v", err)
	}

	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+devToken)
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_AcceptsTokenQueryParam(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	devToken := "tsp_device456"
	if err := store.Add(device.Device{
		ID:       "d_002",
		Name:     "iPad",
		Token:    devToken,
		PairedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to add device: %v", err)
	}

	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodGet, "/api/ws?token="+devToken, nil)
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_RejectsInvalidToken(t *testing.T) {
	store := device.NewStore(filepath.Join(t.TempDir(), "devices.json"))
	mw := NewMiddleware("admin-secret", store)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	mw.Wrap(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "invalid authentication token" {
		t.Errorf("expected error 'invalid authentication token', got %q", body["error"])
	}
}
