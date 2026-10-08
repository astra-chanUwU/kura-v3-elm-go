package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
)

// Actor identifies the capability owner for an authenticated request. It
// aliases auth.Actor so the deployment bearer token still maps to the
// explicit system actor and local development maps to the local actor, while
// session-backed user identity resolves through the same type.
type Actor = auth.Actor

type actorContextKey struct{}

func withActor(r *http.Request, actor Actor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorContextKey{}, actor))
}

func actorFromRequest(r *http.Request) (Actor, bool) {
	actor, ok := r.Context().Value(actorContextKey{}).(Actor)
	return actor, ok && actor.ID != ""
}

func actorContext(expected, provided string) (Actor, bool) {
	return auth.ResolveDeploymentActor(expected, provided)
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
	return auth.SameToken(provided, expected)
}

func bearerToken(header string) string {
	return auth.BearerToken(header)
}
