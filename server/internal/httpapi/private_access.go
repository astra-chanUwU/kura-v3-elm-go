package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
)

// AuthService is the account/session behavior the HTTP layer needs. It is
// satisfied by *auth.Service (over auth.NewPostgresStore) and by fakes in
// tests, so handlers never depend on a database directly.
type AuthService interface {
	Login(ctx context.Context, handle, password string) (auth.LoginResult, error)
	Logout(ctx context.Context, rawToken string) error
	ResolveSession(ctx context.Context, rawToken string) (auth.Actor, auth.User, bool)
	ListUsers(ctx context.Context, actor auth.Actor) ([]auth.User, error)
	CreateUser(ctx context.Context, actor auth.Actor, handle, password, role string) (auth.User, error)
	UpdateUser(ctx context.Context, actor auth.Actor, id int64, patch auth.UpdateUserPatch) (auth.User, error)
	ChangePassword(ctx context.Context, actor auth.Actor, currentPassword, newPassword string) error
}

// AccessConfig configures private-library access control and the session
// context resolver. TrustedOrigin is the single exact public origin for
// cookie CSRF checks (e.g. "https://library.example"); when empty the
// expected origin is derived from the request scheme and Host. Forwarded
// headers are never trusted for either decision.
type AccessConfig struct {
	// PrivateMode enables KURA_PRIVATE_LIBRARY enforcement. When false the
	// Wrap middleware is a pass-through and open/local contracts are exact.
	PrivateMode bool
	// DeploymentToken is the KURA_API_TOKEN bearer; it keeps full admin
	// access in private mode, including CLI reads. Empty means no bearer
	// admin exists (and no implicit local actor in private mode).
	DeploymentToken string
	Service         AuthService
	TrustedOrigin   string
	// AllowHTTPLocal (KURA_AUTH_ALLOW_HTTP_LOCAL) relaxes the Secure
	// cookie flag and the derived origin scheme only for loopback hosts.
	AllowHTTPLocal bool
	SecureCookies  bool
	CookieName     string
}

// RequestActor returns the actor injected by Wrap or SessionContext, for
// coordinator wiring and handlers outside this package's files.
func RequestActor(r *http.Request) (auth.Actor, bool) { return actorFromRequest(r) }

type actorSourceKey struct{}

func actorSource(r *http.Request) string {
	if v, ok := r.Context().Value(actorSourceKey{}).(string); ok {
		return v
	}
	return ""
}

func withActorSource(r *http.Request, actor auth.Actor, source string) *http.Request {
	r = withActor(r, actor)
	return r.WithContext(context.WithValue(r.Context(), actorSourceKey{}, source))
}

// resolveRequestActor resolves identity fresh for this request: deployment
// bearer first (system admin), then the session cookie via the service
// (which re-reads role and disabled state from the store every call). It
// never falls back to the implicit local actor.
func resolveRequestActor(r *http.Request, cfg AccessConfig) (auth.Actor, auth.User, string, bool) {
	provided := bearerToken(r.Header.Get("Authorization"))
	if cfg.DeploymentToken != "" && provided != "" && auth.SameToken(provided, cfg.DeploymentToken) {
		actor := auth.Actor{ID: auth.SystemActorID, System: true, Role: auth.RoleAdmin}
		return actor, auth.User{}, "bearer", true
	}
	if cfg.Service != nil {
		if c, err := r.Cookie(cookieName(cfg.CookieName)); err == nil && c.Value != "" {
			if actor, user, ok := cfg.Service.ResolveSession(r.Context(), c.Value); ok {
				return actor, user, "cookie", true
			}
		}
	}
	return auth.Actor{}, auth.User{}, "", false
}

// SessionContext is the non-blocking session resolver for root wiring: it
// injects the bearer or cookie actor into the request context when one
// resolves and otherwise passes the request through untouched, so it can
// coexist with the legacy requireWriteCapability middleware when private
// mode is off.
func SessionContext(cfg AccessConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if actor, _, source, ok := resolveRequestActor(r, cfg); ok {
				r = withActorSource(r, actor, source)
			}
			next.ServeHTTP(w, r)
		})
	}
}

var privatePublicPaths = map[string]bool{
	"/health":                       true,
	"/api/auth/session":             true,
	"/api/auth/login":               true,
	"/api/auth/logout":              true,
	"/api/auth/invitations/preview": true,
	"/api/auth/register":            true,
	"/api/capabilities":             true,
}

func isModerationPath(path string) bool {
	return path == "/api/posts/moderation" || strings.Contains(path, "/moderation")
}

func isAccountAdminPath(path string) bool {
	return path == "/api/auth/users" || strings.HasPrefix(path, "/api/auth/users/") || path == "/api/auth/invitations" || strings.HasPrefix(path, "/api/auth/invitations/")
}

func isUnsafeMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requestScheme(r *http.Request, cfg AccessConfig) string {
	if r.TLS != nil {
		return "https"
	}
	if cfg.AllowHTTPLocal && isLoopbackHost(r.Host) {
		return "http"
	}
	return "https"
}

// checkCookieCSRF enforces the exact trusted Origin for cookie-authenticated
// unsafe requests. Deployment-bearer requests are non-ambient and skip it;
// the caller only invokes this for cookie source actors.
func checkCookieCSRF(r *http.Request, cfg AccessConfig) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	if cfg.TrustedOrigin != "" {
		return origin == strings.TrimSpace(cfg.TrustedOrigin)
	}
	return origin == requestScheme(r, cfg)+"://"+r.Host
}

// Wrap returns the private-library middleware. When PrivateMode is false
// it only resolves session context (like SessionContext) and never blocks,
// preserving open local and token-only contracts exactly. When true:
//
//   - /health, /api/auth/session, /api/auth/login, /api/auth/logout and
//     /api/capabilities stay public; OPTIONS preflight is passed through.
//   - Every other /api/* and /media/* request needs a deployment bearer
//     (admin) or a valid session cookie, else 401. There is no implicit
//     local actor, even with no deployment token configured.
//   - Viewers read only; editors write; admins additionally reach
//     moderation and /api/auth/users. Published /media visibility is
//     still decided downstream by gatedMedia, which opens the configured
//     store only for a stored same-origin reference. This middleware never
//     fetches external media URLs.
//   - Cookie-authenticated unsafe requests need an exact trusted Origin.
func (cfg AccessConfig) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cfg.PrivateMode {
			if actor, _, source, ok := resolveRequestActor(r, cfg); ok {
				r = withActorSource(r, actor, source)
			}
			next.ServeHTTP(w, r)
			return
		}
		// Private metadata and media must not survive sign-out in shared
		// browser or intermediary caches. Apply this before downstream
		// handlers, including file and conditional-response handlers.
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/media") {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Add("Vary", "Cookie")
			w.Header().Add("Vary", "Authorization")
		}
		if r.Method == http.MethodOptions || privatePublicPaths[r.URL.Path] {
			// Public endpoints still get actor context when resolvable.
			if actor, _, source, ok := resolveRequestActor(r, cfg); ok {
				r = withActorSource(r, actor, source)
			}
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/media") {
			next.ServeHTTP(w, r)
			return
		}
		actor, _, source, ok := resolveRequestActor(r, cfg)
		if !ok {
			w.Header().Set("Cache-Control", "no-store")
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		r = withActorSource(r, actor, source)
		// Capability gates by role.
		switch {
		case r.URL.Path == "/api/auth/password":
			// Any signed-in role may change its own password. Viewers do
			// not have write capability; the handler accepts only the
			// cookie session's account.
		case isSelfProfileMutation(r.Method, r.URL.Path):
			// Any signed-in role may edit its own profile. The handler
			// uses the session user id and never a client-supplied owner.
			// This does not grant collection or library writes.
		case isAccountAdminPath(r.URL.Path):
			if !auth.HasAdminCapability(actor) {
				writeJSONError(w, http.StatusForbidden, "admin capability required")
				return
			}
		case isModerationPath(r.URL.Path):
			if !auth.HasModerateCapability(actor) {
				writeJSONError(w, http.StatusForbidden, "moderation capability required")
				return
			}
		case isUnsafeMethod(r.Method):
			if !auth.HasWriteCapability(actor) {
				writeJSONError(w, http.StatusForbidden, "write capability required")
				return
			}
		default:
			if !auth.HasReadCapability(actor) {
				writeJSONError(w, http.StatusForbidden, "read capability required")
				return
			}
		}
		if source == "cookie" && isUnsafeMethod(r.Method) && !checkCookieCSRF(r, cfg) {
			writeJSONError(w, http.StatusForbidden, "untrusted origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PrivateAccess is the func-form constructor for root wiring:
// r.Use(httpapi.PrivateAccess(cfg)) in private mode, replacing the legacy
// requireWriteCapability middleware; when off it is inert.
func PrivateAccess(cfg AccessConfig) func(http.Handler) http.Handler {
	return cfg.Wrap
}
