package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticationRoutesKeepStaticAllowlistClosed(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "index.html", "Kura authentication shell")
	writeFixture(t, root, "static/brand/auth-observatory.png", "public illustration")
	writeFixture(t, root, "static/brand/private.png", "not public")
	writeFixture(t, root, "static/media/private.png", "private media")
	handler := restrictedStaticHandler(root)
	for _, target := range []string{"/sign-in", "/sign-up", "/trouble"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "Kura authentication shell" {
			t.Fatalf("auth route %s = %d %q", target, rec.Code, rec.Body.String())
		}
	}
	for _, target := range []string{"/static/brand/auth-observatory.png", "/static/brand/private.png", "/static/brand/../media/private.png", "/sign-up/not-a-route"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		want := http.StatusNotFound
		if target == "/static/brand/auth-observatory.png" {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Fatalf("static path %s = %d, want %d", target, rec.Code, want)
		}
	}
}
