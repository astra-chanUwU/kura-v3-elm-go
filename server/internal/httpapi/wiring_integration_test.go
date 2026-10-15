package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
)

func wiringLogin(t *testing.T, router http.Handler, handle, pw string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"handle":"`+handle+`","password":"`+pw+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("router login %s status=%d body=%s", handle, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "kura_session" {
			return c
		}
	}
	t.Fatal("router login set no session cookie")
	return nil
}

func wiringServe(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func wiringCookieReq(method, path, body string, c *http.Cookie) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if c != nil {
		req.AddCookie(c)
	}
	return req
}

func TestWiringPrivateRouterGatesRolesAndMedia(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "published.jpg"), []byte("pub"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := newStubAuth()
	cfg := NewAccountsConfig(stub)
	cfg.PrivateMode = true
	cfg.TrustedOrigin = "https://kura.example"
	mod := &fakeModerator{
		queue:      posts.ModerationQueuePage{Posts: []posts.ModerationQueueItem{}},
		visibility: map[string]posts.MediaVisibility{"/media/uploads/published.jpg": {Referenced: true, Visible: true}},
	}
	simReader := &fakeSimilarityReader{page: posts.SimilarPage{PostID: "7", Algorithm: posts.SimilarityAlgorithm, Matches: []posts.SimilarMatch{}}}
	simIndex := &fakeSimilarityIndexer{
		indexResponse: posts.SimilarityIndexResponse{Posts: []posts.SimilarityIndexResult{{ID: "7", JobID: "11", Status: "pending"}}},
		statusPage:    posts.SimilarityStatusPage{Algorithm: posts.SimilarityAlgorithm, Posts: []posts.SimilarityStatusItem{}},
	}
	deps := Dependencies{
		Posts: &fakeSearcher{}, Moderation: mod, ModerationRead: mod, MediaVisibility: mod,
		Similarity: simReader, SimilarityIndex: simIndex,
		Accounts: &cfg, Recovery: recovery.New(recoveryFixture(t)),
	}
	router := NewRouterWithDependencies("deploy-secret", deps)

	// Anonymous: health/session/capabilities public, everything else 401.
	if rec := wiringServe(router, httptest.NewRequest(http.MethodGet, "/health", nil)); rec.Code != 200 {
		t.Fatalf("anon health %d", rec.Code)
	}
	rec := wiringServe(router, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	var sess map[string]any
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &sess) != nil || sess["private_mode"] != true || sess["authenticated"] != false {
		t.Fatalf("anon session %d %s", rec.Code, rec.Body.String())
	}
	rec = wiringServe(router, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	caps := decodeCapabilities(t, rec.Body)
	if rec.Code != 200 || caps["writes_require_auth"] != true || caps["can_write"] != false || caps["actor"] != nil {
		t.Fatalf("anon private caps %d %v", rec.Code, caps)
	}
	for _, p := range []string{"/api/posts", "/api/posts/7/similar", "/media/uploads/published.jpg", "/api/recovery/backups"} {
		if rec := wiringServe(router, httptest.NewRequest(http.MethodGet, p, nil)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anon GET %s status=%d body=%s", p, rec.Code, rec.Body.String())
		}
	}

	viewer := wiringLogin(t, router, "viewer", "viewer-password-1")
	editor := wiringLogin(t, router, "editor", "editor-password-1")
	admin := wiringLogin(t, router, "admin", "admin-password-1")

	// Viewer: read ok, capabilities cannot write, write/status/recovery denied.
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/posts", "", viewer)); rec.Code != 200 {
		t.Fatalf("viewer read posts %d %s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/posts/7/similar", "", viewer)); rec.Code != 200 {
		t.Fatalf("viewer similar %d %s", rec.Code, rec.Body.String())
	}
	rec = wiringServe(router, wiringCookieReq(http.MethodGet, "/api/capabilities", "", viewer))
	caps = decodeCapabilities(t, rec.Body)
	if caps["can_write"] != false || caps["actor"] != "user:3" {
		t.Fatalf("viewer caps %v", caps)
	}
	req := wiringCookieReq(http.MethodPost, "/api/posts/similarity/index", `{"post_ids":["7"]}`, viewer)
	req.Header.Set("Origin", "https://kura.example")
	if rec := wiringServe(router, req); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer index status=%d %s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/posts/similarity/status", "", viewer)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer similarity status=%d", rec.Code)
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/recovery/backups", "", viewer)); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer recovery status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/media/uploads/published.jpg", "", viewer)); rec.Code != 200 {
		t.Fatalf("viewer published media %d", rec.Code)
	}

	// Editor: can write/index, cannot moderate.
	rec = wiringServe(router, wiringCookieReq(http.MethodGet, "/api/capabilities", "", editor))
	caps = decodeCapabilities(t, rec.Body)
	if caps["can_write"] != true || caps["actor"] != "user:2" {
		t.Fatalf("editor caps %v", caps)
	}
	req = wiringCookieReq(http.MethodPost, "/api/posts/similarity/index", `{"post_ids":["7"]}`, editor)
	req.Header.Set("Origin", "https://kura.example")
	if rec := wiringServe(router, req); rec.Code != 200 {
		t.Fatalf("editor index %d %s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/posts/moderation?state=hidden", "", editor)); rec.Code != http.StatusForbidden {
		t.Fatalf("editor moderation status=%d", rec.Code)
	}

	// Admin: moderation and recovery reachable; deployment bearer is system.
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/posts/moderation?state=hidden", "", admin)); rec.Code != 200 {
		t.Fatalf("admin moderation %d %s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/recovery/backups", "", admin)); rec.Code != 200 {
		t.Fatalf("admin recovery %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	req.Header.Set("Authorization", "Bearer deploy-secret")
	rec = wiringServe(router, req)
	caps = decodeCapabilities(t, rec.Body)
	if caps["can_write"] != true || caps["actor"] != "system" {
		t.Fatalf("bearer caps %v", caps)
	}
	// Logout via router revokes.
	req = wiringCookieReq(http.MethodPost, "/api/auth/logout", "", viewer)
	if rec := wiringServe(router, req); rec.Code != 200 {
		t.Fatalf("logout %d", rec.Code)
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/posts", "", viewer)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout read status=%d", rec.Code)
	}
}

func TestWiringOffModePreservesLegacyAndSessionActorWins(t *testing.T) {
	stub := newStubAuth()
	cfg := NewAccountsConfig(stub) // PrivateMode false
	saved := &fakeSavedSearches{}
	deps := Dependencies{Posts: &fakeSearcher{}, SavedSearches: saved, Accounts: &cfg}
	router := NewRouterWithDependencies("", deps)

	// No cookie: legacy local actor owns saved searches.
	if rec := wiringServe(router, httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)); rec.Code != 200 || saved.owner != "local" {
		t.Fatalf("off local saved %d owner=%q", rec.Code, saved.owner)
	}
	// Login works in open mode and the session actor is not overridden by local.
	viewer := wiringLogin(t, router, "viewer", "viewer-password-1")
	if rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/saved-searches", "", viewer)); rec.Code != 200 || saved.owner != "user:3" {
		t.Fatalf("off viewer saved %d owner=%q", rec.Code, saved.owner)
	}
	rec := wiringServe(router, wiringCookieReq(http.MethodGet, "/api/auth/session", "", viewer))
	var sess map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &sess) != nil || sess["private_mode"] != false || sess["authenticated"] != true {
		t.Fatalf("off session %s", rec.Body.String())
	}
	// Capabilities in off-open mode keep the legacy local contract.
	rec = wiringServe(router, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	caps := decodeCapabilities(t, rec.Body)
	if caps["writes_require_auth"] != false || caps["can_write"] != true || caps["actor"] != "local" {
		t.Fatalf("off caps %v", caps)
	}

	// Token-gated off mode: login stays reachable, legacy write gate intact.
	gated := NewRouterWithDependencies("secret", deps)
	_ = wiringLogin(t, gated, "editor", "editor-password-1")
	editor := wiringLogin(t, gated, "editor", "editor-password-1")
	req := wiringCookieReq(http.MethodPost, "/api/posts/similarity/index", `{"post_ids":["7"]}`, editor)
	// No SimilarityIndex in deps and no bearer: legacy gate must 401 first.
	if rec := wiringServe(gated, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("gated off cookie-only write status=%d %s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(gated, httptest.NewRequest(http.MethodGet, "/api/posts", nil)); rec.Code != 200 {
		t.Fatalf("gated off public read %d", rec.Code)
	}
}

func TestWiringOffModeEditorCannotModerate(t *testing.T) {
	stub := newStubAuth()
	cfg := NewAccountsConfig(stub)
	mod := &fakeModerator{action: posts.ModerationResponse{Posts: []posts.ModerationResult{{ID: "7", Changed: true}}}}
	deps := Dependencies{Posts: &fakeSearcher{}, Moderation: mod, Accounts: &cfg}
	router := NewRouterWithDependencies("", deps)
	editor := wiringLogin(t, router, "editor", "editor-password-1")
	admin := wiringLogin(t, router, "admin", "admin-password-1")
	body := `{"posts":[{"id":"7"}],"action":"hide"}`
	if rec := wiringServe(router, wiringCookieReq(http.MethodPost, "/api/posts/moderation", body, editor)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("editor moderate status=%d %s", rec.Code, rec.Body.String())
	}
	if rec := wiringServe(router, wiringCookieReq(http.MethodPost, "/api/posts/moderation", body, admin)); rec.Code != 200 {
		t.Fatalf("admin moderate status=%d %s", rec.Code, rec.Body.String())
	}
}
