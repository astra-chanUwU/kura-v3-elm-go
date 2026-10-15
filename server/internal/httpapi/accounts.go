package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/go-chi/chi/v5"
)

// AccountsConfig configures the account handlers. SecureCookies defaults
// to true via NewAccountsConfig; AllowHTTPLocal relaxes Secure only for
// loopback request hosts.
type AccountsConfig struct {
	Service         AuthService
	PrivateMode     bool
	DeploymentToken string
	SecureCookies   bool
	AllowHTTPLocal  bool
	TrustedOrigin   string
	CookieName      string
	Limiter         *LoginLimiter
}

// NewAccountsConfig returns a config with secure defaults over service.
func NewAccountsConfig(service AuthService) AccountsConfig {
	return AccountsConfig{Service: service, SecureCookies: true, CookieName: "kura_session"}
}

func (cfg AccountsConfig) access() AccessConfig {
	return AccessConfig{
		PrivateMode: cfg.PrivateMode, DeploymentToken: cfg.DeploymentToken,
		Service: cfg.Service, TrustedOrigin: cfg.TrustedOrigin,
		AllowHTTPLocal: cfg.AllowHTTPLocal, SecureCookies: cfg.SecureCookies,
		CookieName: cfg.CookieName,
	}
}

func cookieName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "kura_session"
	}
	return name
}

func (cfg AccountsConfig) secureForRequest(r *http.Request) bool {
	if cfg.AllowHTTPLocal && isLoopbackHost(r.Host) {
		return false
	}
	return cfg.SecureCookies
}

func (cfg AccountsConfig) setSessionCookie(w http.ResponseWriter, r *http.Request, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName(cfg.CookieName), Value: raw, Path: "/",
		HttpOnly: true, Secure: cfg.secureForRequest(r),
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
	})
}

func (cfg AccountsConfig) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName(cfg.CookieName), Value: "", Path: "/",
		HttpOnly: true, Secure: cfg.secureForRequest(r),
		SameSite: http.SameSiteStrictMode, Expires: time.Unix(0, 0), MaxAge: -1,
	})
}

func setNoStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// --- session discovery shape (frozen) ---

type sessionUserJSON struct {
	ID     int64  `json:"id"`
	Handle string `json:"handle"`
	Role   string `json:"role"`
}

type sessionResponse struct {
	PrivateMode   bool             `json:"private_mode"`
	Authenticated bool             `json:"authenticated"`
	User          *sessionUserJSON `json:"user"`
	CanRead       bool             `json:"can_read"`
	CanWrite      bool             `json:"can_write"`
	CanModerate   bool             `json:"can_moderate"`
	IsAdmin       bool             `json:"is_admin"`
}

func anonymousSession(privateMode bool) sessionResponse {
	return sessionResponse{PrivateMode: privateMode}
}

func sessionFor(cfg AccountsConfig, actor auth.Actor, user auth.User, source string) sessionResponse {
	resp := sessionResponse{PrivateMode: cfg.PrivateMode, Authenticated: true,
		CanRead: auth.HasReadCapability(actor), CanWrite: auth.HasWriteCapability(actor),
		CanModerate: auth.HasModerateCapability(actor), IsAdmin: auth.HasAdminCapability(actor)}
	if source == "cookie" && user.ID > 0 {
		resp.User = &sessionUserJSON{ID: user.ID, Handle: user.Handle, Role: user.Role}
	}
	return resp
}

// resolveForHandlers prefers an actor already injected by Wrap or
// SessionContext, then resolves bearer/cookie directly so handlers also
// work mounted without the middleware.
func (cfg AccountsConfig) resolveForHandlers(r *http.Request) (auth.Actor, auth.User, string, bool) {
	if actor, ok := actorFromRequest(r); ok {
		source := actorSource(r)
		if source == "cookie" && cfg.Service != nil {
			// Cookie actors are re-resolved fresh; an expired, revoked,
			// or disabled session must not fall back to the stale actor
			// injected into the context earlier.
			if c, err := r.Cookie(cookieName(cfg.CookieName)); err == nil {
				if a, u, ok2 := cfg.Service.ResolveSession(r.Context(), c.Value); ok2 {
					return a, u, source, true
				}
			}
			return auth.Actor{}, auth.User{}, "", false
		}
		return actor, auth.User{}, source, true
	}
	return resolveRequestActor(r, cfg.access())
}

// SessionHandler serves GET /api/auth/session (public discovery).
func SessionHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if actor, user, source, ok := cfg.resolveForHandlers(r); ok {
			writeJSON(w, http.StatusOK, sessionFor(cfg, actor, user, source))
			return
		}
		writeJSON(w, http.StatusOK, anonymousSession(cfg.PrivateMode))
	}
}

// --- login rate / concurrency limiter ---

// LoginLimiter bounds login attempts by IP and globally, for both rate
// (per-minute windows) and concurrency. The IP map is pruned and capped
// so attacker-controlled keys cannot grow it without bound.
type LoginLimiter struct {
	mu         sync.Mutex
	window     time.Duration
	perIPRate  int
	globalRate int
	perIPConc  int
	globalConc int
	maxEntries int
	attempts   map[string][]time.Time
	global     []time.Time
	inFlightIP map[string]int
	inFlight   int
	Now        func() time.Time
}

// NewLoginLimiter returns a limiter with moderate defaults sized to
// bcrypt cost: at most 2 concurrent and 10 attempts/minute per IP, and
// 4 concurrent and 60 attempts/minute globally.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{window: time.Minute, perIPRate: 10, globalRate: 60,
		perIPConc: 2, globalConc: 4, maxEntries: 2048,
		attempts: map[string][]time.Time{}, inFlightIP: map[string]int{}}
}

func (l *LoginLimiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func pruneWindow(times []time.Time, cutoff time.Time) []time.Time {
	out := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

// Allow records an attempt and acquires a concurrency slot. The returned
// release func must be called when the login attempt finishes.
func (l *LoginLimiter) Allow(ip string) (func(), bool) {
	if l == nil {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attempts == nil {
		l.attempts = map[string][]time.Time{}
		l.inFlightIP = map[string]int{}
	}
	now := l.now()
	cutoff := now.Add(-l.window)
	l.global = pruneWindow(l.global, cutoff)
	if len(l.attempts) > l.maxEntries {
		for k, v := range l.attempts {
			if v = pruneWindow(v, cutoff); len(v) == 0 {
				delete(l.attempts, k)
			} else {
				l.attempts[k] = v
			}
		}
		// Still oversized with live entries: refuse rather than grow.
		if len(l.attempts) > l.maxEntries {
			return func() {}, false
		}
	}
	recent := pruneWindow(l.attempts[ip], cutoff)
	l.attempts[ip] = recent
	if len(recent) >= l.perIPRate || len(l.global) >= l.globalRate ||
		l.inFlightIP[ip] >= l.perIPConc || l.inFlight >= l.globalConc {
		return func() {}, false
	}
	l.attempts[ip] = append(recent, now)
	l.global = append(l.global, now)
	l.inFlightIP[ip]++
	l.inFlight++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.inFlightIP[ip] > 0 {
				l.inFlightIP[ip]--
			}
			if l.inFlight > 0 {
				l.inFlight--
			}
		})
	}, true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- login / logout ---

const maxAuthBodyBytes = 8 << 10

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request JSON")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request JSON")
		return false
	}
	return true
}

func isJSONRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	media, _, _ := strings.Cut(ct, ";")
	return strings.EqualFold(strings.TrimSpace(media), "application/json")
}

// LoginHandler serves POST /api/auth/login. JSON only: form posts and
// credentials in any other encoding are rejected, the raw session token
// is returned solely as the HttpOnly cookie, and failures are a generic
// 401 with no account enumeration.
func LoginHandler(cfg AccountsConfig) http.HandlerFunc {
	limiter := cfg.Limiter
	if limiter == nil {
		limiter = NewLoginLimiter()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !isJSONRequest(r) {
			writeJSONError(w, http.StatusUnsupportedMediaType, "login requires application/json")
			return
		}
		release, ok := limiter.Allow(clientIP(r))
		if !ok {
			writeJSONError(w, http.StatusTooManyRequests, "too many login attempts")
			return
		}
		defer release()
		if cfg.Service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "accounts unavailable")
			return
		}
		var body struct {
			Handle   string `json:"handle"`
			Password string `json:"password"`
		}
		if !decodeBoundedJSON(w, r, &body) {
			return
		}
		result, err := cfg.Service.Login(r.Context(), body.Handle, body.Password)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		cfg.setSessionCookie(w, r, result.RawToken, result.ExpiresAt)
		writeJSON(w, http.StatusOK, sessionFor(cfg, result.Actor, result.User, "cookie"))
	}
}

// LogoutHandler serves POST /api/auth/logout: it revokes the cookie
// session, expires the cookie, and returns the anonymous discovery shape.
func LogoutHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if cfg.Service != nil {
			if c, err := r.Cookie(cookieName(cfg.CookieName)); err == nil && c.Value != "" {
				_ = cfg.Service.Logout(r.Context(), c.Value)
			}
		}
		cfg.clearSessionCookie(w, r)
		writeJSON(w, http.StatusOK, anonymousSession(cfg.PrivateMode))
	}
}

// --- admin user management ---

type userJSON struct {
	ID       int64  `json:"id"`
	Handle   string `json:"handle"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

func toUserJSON(u auth.User) userJSON {
	return userJSON{ID: u.ID, Handle: u.Handle, Role: u.Role, Disabled: u.Disabled}
}

func (cfg AccountsConfig) requireAdmin(w http.ResponseWriter, r *http.Request) (auth.Actor, bool) {
	actor, _, _, ok := cfg.resolveForHandlers(r)
	if !ok {
		setNoStore(w)
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return auth.Actor{}, false
	}
	if !auth.HasAdminCapability(actor) {
		setNoStore(w)
		writeJSONError(w, http.StatusForbidden, "admin capability required")
		return auth.Actor{}, false
	}
	return actor, true
}

func writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrForbidden):
		writeJSONError(w, http.StatusForbidden, "admin capability required")
	case errors.Is(err, auth.ErrDuplicateHandle):
		writeJSONError(w, http.StatusConflict, "handle already exists")
	case errors.Is(err, auth.ErrLastAdmin), errors.Is(err, auth.ErrSelfAction):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, auth.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, auth.ErrInvalidPassword), errors.Is(err, auth.ErrInvalidHandle),
		errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrReservedHandle),
		errors.Is(err, auth.ErrInvalidUserID), errors.Is(err, auth.ErrNoChange):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "account operation failed")
	}
}

// ListUsersHandler serves GET /api/auth/users (admin only).
func ListUsersHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor, ok := cfg.requireAdmin(w, r)
		if !ok {
			return
		}
		if cfg.Service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "accounts unavailable")
			return
		}
		users, err := cfg.Service.ListUsers(r.Context(), actor)
		if err != nil {
			writeAccountError(w, err)
			return
		}
		out := make([]userJSON, 0, len(users))
		for _, u := range users {
			out = append(out, toUserJSON(u))
		}
		writeJSON(w, http.StatusOK, struct {
			Users []userJSON `json:"users"`
		}{Users: out})
	}
}

// CreateUserHandler serves POST /api/auth/users (admin only).
func CreateUserHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor, ok := cfg.requireAdmin(w, r)
		if !ok {
			return
		}
		if !isJSONRequest(r) {
			writeJSONError(w, http.StatusUnsupportedMediaType, "request requires application/json")
			return
		}
		if cfg.Service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "accounts unavailable")
			return
		}
		var body struct {
			Handle   string `json:"handle"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if !decodeBoundedJSON(w, r, &body) {
			return
		}
		user, err := cfg.Service.CreateUser(r.Context(), actor, body.Handle, body.Password, body.Role)
		if err != nil {
			writeAccountError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			User userJSON `json:"user"`
		}{User: toUserJSON(user)})
	}
}

func patchUserID(r *http.Request) string {
	if id := chi.URLParam(r, "id"); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return parts[len(parts)-1]
}

// UpdateUserHandler serves PATCH /api/auth/users/{id} (admin only). The
// body is strict: only role/disabled/password, at least one present, and
// explicit nulls are rejected so no field changes by accident.
func UpdateUserHandler(cfg AccountsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor, ok := cfg.requireAdmin(w, r)
		if !ok {
			return
		}
		if !isJSONRequest(r) {
			writeJSONError(w, http.StatusUnsupportedMediaType, "request requires application/json")
			return
		}
		if cfg.Service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "accounts unavailable")
			return
		}
		id, err := auth.ParseCanonicalUserID(patchUserID(r))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "user id must be a positive integer")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAuthBodyBytes))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request JSON")
			return
		}
		var fields map[string]json.RawMessage
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		if err := dec.Decode(&fields); err != nil || dec.Decode(&struct{}{}) != io.EOF {
			writeJSONError(w, http.StatusBadRequest, "invalid request JSON")
			return
		}
		if len(fields) == 0 {
			writeJSONError(w, http.StatusBadRequest, "no changes supplied")
			return
		}
		var patch auth.UpdateUserPatch
		for key, value := range fields {
			if strings.TrimSpace(string(value)) == "null" {
				writeJSONError(w, http.StatusBadRequest, "field "+key+" must not be null")
				return
			}
			switch key {
			case "role":
				var v string
				if err := json.Unmarshal(value, &v); err != nil {
					writeJSONError(w, http.StatusBadRequest, "role must be a string")
					return
				}
				patch.Role = &v
			case "disabled":
				var v bool
				if err := json.Unmarshal(value, &v); err != nil {
					writeJSONError(w, http.StatusBadRequest, "disabled must be a boolean")
					return
				}
				patch.Disabled = &v
			case "password":
				var v string
				if err := json.Unmarshal(value, &v); err != nil {
					writeJSONError(w, http.StatusBadRequest, "password must be a string")
					return
				}
				patch.Password = &v
			default:
				writeJSONError(w, http.StatusBadRequest, "unknown field "+key)
				return
			}
		}
		user, err := cfg.Service.UpdateUser(r.Context(), actor, id, patch)
		if err != nil {
			writeAccountError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			User userJSON `json:"user"`
		}{User: toUserJSON(user)})
	}
}

// AccountsHandlers groups the account handlers for coordinator wiring.
type AccountsHandlers struct {
	Session    http.HandlerFunc
	Login      http.HandlerFunc
	Logout     http.HandlerFunc
	ListUsers  http.HandlerFunc
	CreateUser http.HandlerFunc
	UpdateUser http.HandlerFunc
}

// NewAccountsHandlers builds every account handler from one config.
func NewAccountsHandlers(cfg AccountsConfig) AccountsHandlers {
	if cfg.CookieName == "" {
		cfg.CookieName = "kura_session"
	}
	return AccountsHandlers{
		Session: SessionHandler(cfg), Login: LoginHandler(cfg), Logout: LogoutHandler(cfg),
		ListUsers: ListUsersHandler(cfg), CreateUser: CreateUserHandler(cfg), UpdateUser: UpdateUserHandler(cfg),
	}
}

// RegisterAuthRoutes mounts the frozen /auth/* shapes on an /api router.
// Coordinator wiring:
//
//	api.Route("/auth", ...) is NOT used; call this on the /api router so
//	paths become /api/auth/session, /api/auth/login, /api/auth/logout and
//	/api/auth/users[/{id}].
func RegisterAuthRoutes(api chi.Router, cfg AccountsConfig) {
	h := NewAccountsHandlers(cfg)
	api.Get("/auth/session", h.Session)
	api.Post("/auth/login", h.Login)
	api.Post("/auth/logout", h.Logout)
	api.Get("/auth/users", h.ListUsers)
	api.Post("/auth/users", h.CreateUser)
	api.Patch("/auth/users/{id}", h.UpdateUser)
}
