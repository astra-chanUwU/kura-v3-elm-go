package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeCapabilities(t *testing.T, body *bytes.Buffer) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.NewDecoder(body).Decode(&decoded); err != nil {
		t.Fatalf("decode capabilities: %v body=%s", err, body.String())
	}
	return decoded
}

func TestCapabilitiesOpenLocalNoBearer(t *testing.T) {
	router := NewRouterWithToken("", &fakeSearcher{})
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open no bearer status=%d body=%s", rec.Code, rec.Body.String())
	}
	decoded := decodeCapabilities(t, rec.Body)
	if decoded["writes_require_auth"] != false {
		t.Errorf("writes_require_auth=%v want false", decoded["writes_require_auth"])
	}
	if decoded["can_write"] != true {
		t.Errorf("can_write=%v want true", decoded["can_write"])
	}
	if decoded["actor"] != "local" {
		t.Errorf("actor=%v want local", decoded["actor"])
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("response leaked secret")
	}
	// Open mode must not leak token digest
	if rec.Header().Get("Content-Type") == "" {
		t.Error("missing content-type")
	}
}

func TestCapabilitiesGatedNoBearer(t *testing.T) {
	router := NewRouterWithToken("secret", &fakeSearcher{})
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gated no bearer status=%d body=%s", rec.Code, rec.Body.String())
	}
	decoded := decodeCapabilities(t, rec.Body)
	if decoded["writes_require_auth"] != true {
		t.Errorf("writes_require_auth=%v want true", decoded["writes_require_auth"])
	}
	if decoded["can_write"] != false {
		t.Errorf("can_write=%v want false", decoded["can_write"])
	}
	if decoded["actor"] != nil {
		t.Errorf("actor=%v want null", decoded["actor"])
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("response leaked secret")
	}
}

func TestCapabilitiesGatedValidBearer(t *testing.T) {
	router := NewRouterWithToken("secret", &fakeSearcher{})
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gated valid status=%d body=%s", rec.Code, rec.Body.String())
	}
	decoded := decodeCapabilities(t, rec.Body)
	if decoded["writes_require_auth"] != true {
		t.Errorf("writes_require_auth=%v want true", decoded["writes_require_auth"])
	}
	if decoded["can_write"] != true {
		t.Errorf("can_write=%v want true", decoded["can_write"])
	}
	if decoded["actor"] != "system" {
		t.Errorf("actor=%v want system", decoded["actor"])
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("response leaked secret")
	}
}

func TestCapabilitiesGatedInvalidBearer(t *testing.T) {
	router := NewRouterWithToken("secret", &fakeSearcher{})
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("gated invalid status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if errBody["error"] != "write capability required" {
		t.Errorf("error=%q want write capability required", errBody["error"])
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("response leaked secret")
	}
}

func TestCapabilitiesGatedMalformedBearerNotMistakenForAbsent(t *testing.T) {
	router := NewRouterWithToken("secret", &fakeSearcher{})
	malformed := []string{
		"",
		" ",
		"\t",
		"Bearer",
		"Bearer ",
		"Bearer  ",
		"Basic abc",
		"Bearer a b",
		"Token secret",
		"Bearer\ttoken\n",
		" Bearersecret",
	}
	for _, header := range malformed {
		req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status=%d want 401 body=%s", header, rec.Code, rec.Body.String())
		}
		var errBody map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
			t.Errorf("header %q: decode error %v", header, err)
			continue
		}
		if errBody["error"] != "write capability required" {
			t.Errorf("header %q: error=%q want write capability required", header, errBody["error"])
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("header %q leaked secret", header)
		}
	}
	// Ensure absent still returns 200 not 401
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("absent after malformed checks: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilitiesDoesNotContainSecretOrDigest(t *testing.T) {
	router := NewRouterWithToken("s3cr3t-value", &fakeSearcher{})
	digest := sha256.Sum256([]byte("s3cr3t-value"))
	digestHex := hex.EncodeToString(digest[:])
	cases := []struct {
		name   string
		header string
	}{
		{"no auth", ""},
		{"valid", "Bearer s3cr3t-value"},
		{"invalid", "Bearer wrong"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		body := rec.Body.String()
		if strings.Contains(body, "s3cr3t-value") || strings.Contains(body, "secret") {
			t.Errorf("%s leaked token: %s", tc.name, body)
		}
		if strings.Contains(strings.ToLower(body), digestHex) {
			t.Errorf("%s leaked token digest", tc.name)
		}
	}
}

func TestCapabilitiesPublicReadsRemainPublicWhenGated(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "plain.jpg"), []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	gated := NewRouterWithToken("secret", &fakeSearcher{})
	// Search
	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public search gated no auth status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Health
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health gated status=%d", rec.Code)
	}
	// Media public (no checker)
	req = httptest.NewRequest(http.MethodGet, "/media/uploads/plain.jpg", nil)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("media public gated status=%d", rec.Code)
	}
}

func TestCapabilitiesSavedSearchReadOwnership(t *testing.T) {
	fake := &fakeSavedSearches{}
	gated := NewRouterWithToken("secret", fake)
	// Missing auth should be 401
	req := httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("saved search missing auth status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Valid token should succeed and attribute to system
	req = httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("saved search valid status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.owner != "system" {
		t.Errorf("saved search owner=%q want system", fake.owner)
	}
	// Invalid token should be 401
	req = httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("saved search invalid status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Open mode local
	open := NewRouter(fake)
	req = httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)
	rec = httptest.NewRecorder()
	open.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open saved search status=%d", rec.Code)
	}
	if fake.owner != "local" {
		t.Errorf("open owner=%q want local", fake.owner)
	}
}

func TestCapabilitiesProtectedMutationBehavior(t *testing.T) {
	fake := &fakeCollections{}
	gated := NewRouterWithToken("secret", fake)
	// Missing
	req := httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Private"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mutation missing status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Invalid
	req = httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Private"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mutation invalid status=%d", rec.Code)
	}
	// Malformed
	req = httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Private"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer")
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mutation malformed status=%d", rec.Code)
	}
	// Valid
	req = httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Private"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mutation valid status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Ensure capabilities itself does not require write gate on GET
	req = httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities missing still 200 status=%d", rec.Code)
	}
}

func TestPatchPreflightWithAuthorization(t *testing.T) {
	// Gated router should still allow OPTIONS preflight regardless of auth
	router := NewRouterWithToken("secret", &fakeSavedSearches{})
	// Preflight for PATCH with Authorization header
	req := httptest.NewRequest(http.MethodOptions, "/api/saved-searches/1", nil)
	req.Header.Set("Origin", "http://localhost:8000")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status=%d body=%s", rec.Code, rec.Body.String())
	}
	allowMethods := rec.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowMethods, "PATCH") {
		t.Errorf("Allow-Methods=%q missing PATCH", allowMethods)
	}
	if !strings.Contains(allowMethods, "GET") || !strings.Contains(allowMethods, "POST") || !strings.Contains(allowMethods, "DELETE") || !strings.Contains(allowMethods, "OPTIONS") {
		t.Errorf("Allow-Methods=%q missing expected methods", allowMethods)
	}
	allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "Authorization") || !strings.Contains(allowHeaders, "Content-Type") {
		t.Errorf("Allow-Headers=%q missing expected headers", allowHeaders)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8000" {
		t.Errorf("Allow-Origin=%q want http://localhost:8000", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	// Non-allowed origin should still return PATCH but no origin header
	req = httptest.NewRequest(http.MethodOptions, "/api/saved-searches/1", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("evil origin should not get Allow-Origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Error("PATCH missing even for evil origin preflight")
	}
	// Actual PATCH still protected
	req = httptest.NewRequest(http.MethodPatch, "/api/saved-searches/1", bytes.NewBufferString(`{"name":"Updated"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH without auth status=%d want 401", rec.Code)
	}
	// PATCH with valid auth should reach handler (BadRequest due to validation maybe, but not 401)
	req = httptest.NewRequest(http.MethodPatch, "/api/saved-searches/1", bytes.NewBufferString(`{"name":"Updated"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("PATCH with valid auth incorrectly 401 body=%s", rec.Body.String())
	}
}

func TestCapabilitiesPreservesExistingAllowedOriginsAndHeaders(t *testing.T) {
	router := NewRouter(&fakeSearcher{})
	// 127.0.0.1 origin should be allowed
	req := httptest.NewRequest(http.MethodOptions, "/api/posts", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8000")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:8000" {
		t.Errorf("127 origin not allowed: %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Errorf("Vary missing Origin: %q", vary)
	}
	// Check public reads still public through middleware
	req = httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	req.Header.Set("Origin", "http://localhost:8000")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public search status=%d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8000" {
		t.Errorf("GET origin not echoed: %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

// Ensure media and search handlers still delegate correctly via router embedding.
func TestCapabilitiesDoesNotInterfereWithRouterEmbedding(t *testing.T) {
	// Verify that NewRouter still works with nil searcher for capabilities
	router := NewRouterWithToken("secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities with nil searcher status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("capabilities invalid with nil searcher status=%d", rec.Code)
	}
}
