package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// Actor identifies the capability owner for an authenticated request. Until
// user sessions exist, the configured deployment token maps to the explicit
// system actor and local development maps to the local actor.
type Actor struct {
	ID string
}

type actorContextKey struct{}

func withActor(r *http.Request, actor Actor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorContextKey{}, actor))
}

func actorFromRequest(r *http.Request) (Actor, bool) {
	actor, ok := r.Context().Value(actorContextKey{}).(Actor)
	return actor, ok && actor.ID != ""
}

func actorContext(expected, provided string) (Actor, bool) {
	if expected == "" {
		return Actor{ID: "local"}, true
	}
	if sameToken(provided, expected) {
		return Actor{ID: "system"}, true
	}
	return Actor{}, false
}

// requireWriteCapability protects mutating API routes when KURA_API_TOKEN is
// configured. Reads, health, media, and CORS preflight remain public so the
// current local browser workflow keeps working without credentials.
func requireWriteCapability(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := bearerToken(r.Header.Get("Authorization"))
			if actor, ok := actorContext(expected, provided); ok {
				r = withActor(r, actor)
			}
			if expected == "" {
				next.ServeHTTP(w, r)
				return
			}
			if r.URL.Path == "/health" || !strings.HasPrefix(r.URL.Path, "/api/") || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
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
