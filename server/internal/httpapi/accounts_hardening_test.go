package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestResolveForHandlersExpiredCookieNoStaleFallbackHardening(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	cookie := loginCookie(t, cfg, "viewer", "viewer-password-1")
	stale := stub.users[3].Actor()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(cookie)
	req = withActorSource(req, stale, "cookie")
	if _, _, _, ok := cfg.resolveForHandlers(req); !ok {
		t.Fatal("fresh cookie actor did not resolve")
	}

	stub.expired[cookie.Value] = true
	req2 := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req2.AddCookie(cookie)
	req2 = withActorSource(req2, stale, "cookie")
	if actor, _, _, ok := cfg.resolveForHandlers(req2); ok {
		t.Fatalf("expired cookie fell back to stale actor %+v", actor)
	}

	rec := httptest.NewRecorder()
	SessionHandler(cfg)(rec, req2)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authenticated":false`) {
		t.Fatalf("session after expiry = %d %s", rec.Code, rec.Body.String())
	}
}

func TestResolveForHandlersBearerContextKeptHardening(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	bearer := stub.users[1].Actor()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/users", nil)
	req = withActorSource(req, bearer, "bearer")
	actor, _, source, ok := cfg.resolveForHandlers(req)
	if !ok || actor.ID != bearer.ID || source != "bearer" {
		t.Fatalf("bearer context = %+v %q %v", actor, source, ok)
	}
}

func TestLoginLimiterModerateLimitsHardening(t *testing.T) {
	lim := NewLoginLimiter()
	r1, ok := lim.Allow("10.0.0.1")
	if !ok {
		t.Fatal("first concurrent login rejected")
	}
	r2, ok := lim.Allow("10.0.0.1")
	if !ok {
		t.Fatal("second concurrent login rejected")
	}
	if _, ok := lim.Allow("10.0.0.1"); ok {
		t.Fatal("third concurrent login from same IP allowed, want per-IP concurrency 2")
	}
	r1()
	r2()

	global := NewLoginLimiter()
	var releases []func()
	for _, ip := range []string{"10.0.1.1", "10.0.1.2", "10.0.1.3", "10.0.1.4"} {
		rel, ok := global.Allow(ip)
		if !ok {
			t.Fatalf("global concurrent login from %s rejected", ip)
		}
		releases = append(releases, rel)
	}
	if _, ok := global.Allow("10.0.1.5"); ok {
		t.Fatal("fifth global concurrent login allowed, want global concurrency 4")
	}
	for _, rel := range releases {
		rel()
	}

	rated := NewLoginLimiter()
	for i := 0; i < 10; i++ {
		rel, ok := rated.Allow("10.0.2.1")
		if !ok {
			t.Fatalf("attempt %d rejected before per-IP rate 10", i+1)
		}
		rel()
	}
	if _, ok := rated.Allow("10.0.2.1"); ok {
		t.Fatal("eleventh attempt in a minute allowed, want per-IP rate 10")
	}
	if rel, ok := rated.Allow("10.0.2.2"); !ok {
		t.Fatal("different IP rejected by per-IP rate")
	} else {
		rel()
	}

	globalRated := NewLoginLimiter()
	for i := 0; i < 60; i++ {
		ip := "10.0.3." + strconv.Itoa(i%20+1)
		rel, ok := globalRated.Allow(ip)
		if !ok {
			t.Fatalf("global attempt %d rejected before global rate 60", i+1)
		}
		rel()
	}
	if _, ok := globalRated.Allow("10.0.3.99"); ok {
		t.Fatal("61st global attempt in a minute allowed, want global rate 60")
	}
}
