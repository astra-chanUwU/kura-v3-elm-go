package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivateResponsesCannotBeCached(t *testing.T) {
	for _, path := range []string{"/api/posts", "/media/original/fixture.png"} {
		t.Run(path, func(t *testing.T) {
			handler := (AccessConfig{PrivateMode: true, DeploymentToken: "fixture-token"}).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("private fixture"))
			}))
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer fixture-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
				t.Fatalf("private response was cacheable: code=%d, cache=%q", response.Code, response.Header().Get("Cache-Control"))
			}
			vary := strings.Join(response.Header().Values("Vary"), ",")
			if !strings.Contains(vary, "Cookie") || !strings.Contains(vary, "Authorization") {
				t.Fatalf("private response did not vary by identity: %q", vary)
			}
		})
	}
}
