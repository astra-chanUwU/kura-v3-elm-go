package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireWriteCapability protects mutating API routes when KURA_API_TOKEN is
// configured. Reads, health, media, and CORS preflight remain public so the
// current local browser workflow keeps working without credentials.
func requireWriteCapability(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if expected == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || !strings.HasPrefix(r.URL.Path, "/api/") || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			provided := bearerToken(r.Header.Get("Authorization"))
			if !sameToken(provided, expected) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="kura"`)
				writeJSONError(w, http.StatusUnauthorized, "write capability required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameToken(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
