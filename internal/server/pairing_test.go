package server

import (
	"encoding/json"
	"fmt"
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
		t.Fatalf("step 1: expected 401 without auth, got %d", w.Code)
	}

	// 2. Initiate pairing with admin auth
	body := `{"name":"Integration Test Phone"}`
	req = httptest.NewRequest("POST", "/api/pair/initiate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tsp_admin_testtoken")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("step 2: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var initiateResp struct {
		Code    string `json:"code"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(w.Body).Decode(&initiateResp); err != nil {
		t.Fatalf("step 2: failed to decode initiate response: %v", err)
	}
	if initiateResp.Code == "" {
		t.Fatal("step 2: pairing code is empty")
	}
	pairingCode := initiateResp.Code
	t.Logf("pairing code: %s", pairingCode)

	// 3. Complete pairing without auth (open endpoint)
	body = fmt.Sprintf(`{"code":%q,"name":"Integration Test Phone"}`, pairingCode)
	req = httptest.NewRequest("POST", "/api/pair/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header — this endpoint is open
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("step 3: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var completeResp struct {
		Token    string `json:"token"`
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&completeResp); err != nil {
		t.Fatalf("step 3: failed to decode complete response: %v", err)
	}
	if completeResp.Token == "" {
		t.Fatal("step 3: device token is empty")
	}
	if completeResp.DeviceID == "" {
		t.Fatal("step 3: device_id is empty")
	}
	deviceToken := completeResp.Token
	t.Logf("device token: %s, device_id: %s", deviceToken, completeResp.DeviceID)

	// 4. Use device token to access protected endpoint
	req = httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", deviceToken))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("step 4: expected 200 with device token, got %d; body: %s", w.Code, w.Body.String())
	}

	// 5. Same code should not work again
	body = fmt.Sprintf(`{"code":%q,"name":"Integration Test Phone"}`, pairingCode)
	req = httptest.NewRequest("POST", "/api/pair/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("step 5: expected 401 for reused code, got %d; body: %s", w.Code, w.Body.String())
	}

	// 6. Pair status should show claimed
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/pair/status?code=%s", pairingCode), nil)
	req.Header.Set("Authorization", "Bearer tsp_admin_testtoken")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("step 6: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var statusResp struct {
		Claimed    bool   `json:"claimed"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&statusResp); err != nil {
		t.Fatalf("step 6: failed to decode status response: %v", err)
	}
	if !statusResp.Claimed {
		t.Fatal("step 6: expected claimed=true, got false")
	}
	if statusResp.DeviceName != "Integration Test Phone" {
		t.Fatalf("step 6: expected device_name=%q, got %q", "Integration Test Phone", statusResp.DeviceName)
	}
}
