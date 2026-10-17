package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
)

func privateCfg(stub *stubAuth) AccessConfig {
	return AccessConfig{PrivateMode: true, DeploymentToken: "deploy" + "-secret",
		Service: stub, TrustedOrigin: "https://kura.example", SecureCookies: true}
}

func downstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, _ := actorFromRequest(r)
		w.Header().Set("X-Actor", actor.ID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func cookieReq(t *testing.T, stub *stubAuth, cfg AccountsConfig, handle, pw, method, path string) *http.Request {
	t.Helper()
	c := loginCookie(t, cfg, handle, pw)
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.AddCookie(c)
	return req
}

func TestPrivateModeBoundariesAndCSRF(t *testing.T) {
	stub := newStubAuth()
	acfg := testAccountsConfig(stub)
	cfg := privateCfg(stub)
	handler := cfg.Wrap(downstream())

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Anonymous reads, media, and mutations are all 401 in private mode.
	for _, p := range []string{"/api/posts", "/media/a.jpg", "/api/saved-searches", "/api/posts/export", "/api/jobs/1"} {
		if rec := serve(httptest.NewRequest(http.MethodGet, p, nil)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anon GET %s status=%d", p, rec.Code)
		}
	}
	// Public discovery paths pass anonymously.
	for _, p := range []string{"/health", "/api/auth/session", "/api/auth/login", "/api/capabilities"} {
		if rec := serve(httptest.NewRequest(http.MethodGet, p, nil)); rec.Code == http.StatusUnauthorized {
			t.Fatalf("public %s blocked", p)
		}
	}
	if rec := serve(httptest.NewRequest(http.MethodOptions, "/api/posts", nil)); rec.Code == http.StatusUnauthorized {
		t.Fatal("OPTIONS preflight blocked")
	}

	// Viewer: read ok, write forbidden.
	req := cookieReq(t, stub, acfg, "viewer", "viewer-password-1", http.MethodGet, "/api/posts")
	if rec := serve(req); rec.Code != 200 || rec.Header().Get("X-Actor") != "user:3" {
		t.Fatalf("viewer read %d actor=%q", rec.Code, rec.Header().Get("X-Actor"))
	}
	req = cookieReq(t, stub, acfg, "viewer", "viewer-password-1", http.MethodPost, "/api/collections")
	req.Header.Set("Origin", "https://kura.example")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer write status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "viewer", "viewer-password-1", http.MethodPost, "/api/auth/password")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer password change without origin status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "viewer", "viewer-password-1", http.MethodPost, "/api/auth/password")
	req.Header.Set("Origin", "https://kura.example")
	if rec := serve(req); rec.Code != http.StatusOK {
		t.Fatalf("viewer password change status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := serve(httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{}`))); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous password change status=%d", rec.Code)
	}

	// Editor: write requires exact trusted Origin (CSRF).
	req = cookieReq(t, stub, acfg, "editor", "editor-password-1", http.MethodPost, "/api/collections")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("editor write without origin status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "editor", "editor-password-1", http.MethodPost, "/api/collections")
	req.Header.Set("Origin", "https://evil.example")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("editor write evil origin status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "editor", "editor-password-1", http.MethodPost, "/api/collections")
	req.Header.Set("Origin", "https://kura.example")
	if rec := serve(req); rec.Code != 200 {
		t.Fatalf("editor write trusted origin status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "admin", "admin-password-1", http.MethodPost, "/api/recovery/backups")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("admin backup create without origin status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "admin", "admin-password-1", http.MethodPost, "/api/recovery/backups")
	req.Header.Set("Origin", "https://kura.example")
	if rec := serve(req); rec.Code != 200 {
		t.Fatalf("admin backup create trusted origin status=%d", rec.Code)
	}
	// Editor cannot moderate or administer users.
	req = cookieReq(t, stub, acfg, "editor", "editor-password-1", http.MethodGet, "/api/posts/moderation")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("editor moderation status=%d", rec.Code)
	}
	req = cookieReq(t, stub, acfg, "editor", "editor-password-1", http.MethodGet, "/api/auth/users")
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Fatalf("editor users status=%d", rec.Code)
	}
	// Admin cookie reaches moderation and users.
	req = cookieReq(t, stub, acfg, "admin", "admin-password-1", http.MethodGet, "/api/posts/1/moderation/preview")
	if rec := serve(req); rec.Code != 200 {
		t.Fatalf("admin preview status=%d", rec.Code)
	}

	// Deployment bearer is admin and skips cookie CSRF entirely.
	req = httptest.NewRequest(http.MethodPost, "/api/collections", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+"deploy"+"-secret")
	if rec := serve(req); rec.Code != 200 || rec.Header().Get("X-Actor") != "system" {
		t.Fatalf("bearer write %d actor=%q", rec.Code, rec.Header().Get("X-Actor"))
	}
	req = httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	req.Header.Set("Authorization", "Bearer "+"wrong-token")
	if rec := serve(req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer status=%d", rec.Code)
	}

	// Expired/revoked session cookie no longer resolves (fresh check).
	c := loginCookie(t, acfg, "viewer", "viewer-password-1")
	stub.expired[c.Value] = true
	req = httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	req.AddCookie(c)
	if rec := serve(req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired cookie status=%d", rec.Code)
	}
}

func TestPrivateModeNoImplicitLocalAndOpenModePassThrough(t *testing.T) {
	stub := newStubAuth()
	// Private mode with NO deployment token: still no local actor.
	cfg := AccessConfig{PrivateMode: true, Service: stub, TrustedOrigin: "https://kura.example"}
	rec := httptest.NewRecorder()
	cfg.Wrap(downstream()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/posts", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("private no-token anon status=%d (implicit local leaked)", rec.Code)
	}
	// Open mode: Wrap never blocks anonymous requests.
	open := AccessConfig{PrivateMode: false, Service: stub}
	rec = httptest.NewRecorder()
	open.Wrap(downstream()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/posts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("open mode status=%d", rec.Code)
	}
	// Loopback HTTP relaxation is loopback-only.
	if !isLoopbackHost("127.0.0.1:8080") || !isLoopbackHost("localhost:8000") || isLoopbackHost("example.com:443") {
		t.Fatal("loopback detection wrong")
	}
	_ = auth.RoleViewer
}
