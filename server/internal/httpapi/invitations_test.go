package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicInvitationEndpointsRequireSameOriginAndJSON(t *testing.T) {
	cfg := NewAccountsConfig(newStubAuth())
	cfg.TrustedOrigin = "https://kura.example"
	preview := PreviewInvitationHandler(cfg)

	tests := []struct {
		name, contentType, origin string
		want                      int
	}{
		{"cross origin", "application/json", "https://attacker.example", http.StatusForbidden},
		{"missing origin", "application/json", "", http.StatusForbidden},
		{"wrong content type", "text/plain", "https://kura.example", http.StatusUnsupportedMediaType},
		{"trusted origin", "application/json", "https://kura.example", http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://kura.example/api/auth/invitations/preview", strings.NewReader(`{"token":"opaque"}`))
			req.Header.Set("Content-Type", tc.contentType)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			preview.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestPublicInvitationJSONRejectsUnknownFields(t *testing.T) {
	cfg := NewAccountsConfig(newStubAuth())
	cfg.TrustedOrigin = "https://kura.example"
	req := httptest.NewRequest(http.MethodPost, "https://kura.example/api/auth/register", strings.NewReader(`{"token":"t","handle":"h","password":"long-enough-password","role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://kura.example")
	rec := httptest.NewRecorder()
	RegisterInvitationHandler(cfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
