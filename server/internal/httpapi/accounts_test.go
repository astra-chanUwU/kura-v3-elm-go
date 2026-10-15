package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/go-chi/chi/v5"
)

// stubAuth is an in-memory AuthService for HTTP tests.
type stubAuth struct {
	users    map[int64]auth.User
	password map[int64]string
	sessions map[string]auth.User // raw token -> user
	expired  map[string]bool
	nextID   int64
}

func newStubAuth() *stubAuth {
	s := &stubAuth{users: map[int64]auth.User{}, password: map[int64]string{}, sessions: map[string]auth.User{}, expired: map[string]bool{}, nextID: 1}
	s.add("admin", "admin-password-1", "admin", false)
	s.add("editor", "editor-password-1", "editor", false)
	s.add("viewer", "viewer-password-1", "viewer", false)
	return s
}

func (s *stubAuth) add(handle, pw, role string, disabled bool) auth.User {
	u := auth.User{ID: s.nextID, Handle: handle, Role: role, Disabled: disabled}
	s.nextID++
	s.users[u.ID] = u
	s.password[u.ID] = pw
	return u
}

func (s *stubAuth) Login(_ context.Context, handle, pw string) (auth.LoginResult, error) {
	for _, u := range s.users {
		if u.Handle == handle {
			if u.Disabled || s.password[u.ID] != pw {
				return auth.LoginResult{}, auth.ErrInvalidCredentials
			}
			raw := "raw-token-for-" + handle
			s.sessions[raw] = u
			return auth.LoginResult{User: u, Actor: u.Actor(), RawToken: raw, ExpiresAt: time.Now().Add(auth.SessionTTL)}, nil
		}
	}
	return auth.LoginResult{}, auth.ErrInvalidCredentials
}

func (s *stubAuth) Logout(_ context.Context, raw string) error { delete(s.sessions, raw); return nil }

func (s *stubAuth) ResolveSession(_ context.Context, raw string) (auth.Actor, auth.User, bool) {
	if s.expired[raw] {
		return auth.Actor{}, auth.User{}, false
	}
	u, ok := s.sessions[raw]
	if !ok || u.Disabled {
		return auth.Actor{}, auth.User{}, false
	}
	// Fresh role/disabled from the user table.
	if cur, ok := s.users[u.ID]; ok {
		u = cur
	}
	return u.Actor(), u, true
}

func (s *stubAuth) ListUsers(_ context.Context, actor auth.Actor) ([]auth.User, error) {
	if !auth.HasAdminCapability(actor) {
		return nil, auth.ErrForbidden
	}
	out := []auth.User{}
	for _, u := range s.users {
		out = append(out, u)
	}
	return out, nil
}

func (s *stubAuth) CreateUser(_ context.Context, actor auth.Actor, handle, pw, role string) (auth.User, error) {
	if !auth.HasAdminCapability(actor) {
		return auth.User{}, auth.ErrForbidden
	}
	if err := auth.ValidateHandle(handle); err != nil {
		return auth.User{}, err
	}
	if !auth.ValidRole(role) {
		return auth.User{}, auth.ErrInvalidRole
	}
	if err := auth.ValidatePassword(pw); err != nil {
		return auth.User{}, err
	}
	for _, u := range s.users {
		if u.Handle == handle {
			return auth.User{}, auth.ErrDuplicateHandle
		}
	}
	return s.add(handle, pw, role, false), nil
}

func (s *stubAuth) UpdateUser(_ context.Context, actor auth.Actor, id int64, patch auth.UpdateUserPatch) (auth.User, error) {
	if !auth.HasAdminCapability(actor) {
		return auth.User{}, auth.ErrForbidden
	}
	u, ok := s.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	if patch.Password != nil {
		if err := auth.ValidatePassword(*patch.Password); err != nil {
			return auth.User{}, err
		}
		s.password[id] = *patch.Password
		for raw, sess := range s.sessions {
			if sess.ID == id {
				delete(s.sessions, raw)
			}
		}
	}
	if patch.Role != nil {
		u.Role = *patch.Role
	}
	if patch.Disabled != nil {
		u.Disabled = *patch.Disabled
	}
	s.users[id] = u
	return u, nil
}

func testAccountsConfig(s *stubAuth) AccountsConfig {
	cfg := NewAccountsConfig(s)
	cfg.PrivateMode = true
	cfg.TrustedOrigin = "https://kura.example"
	return cfg
}

func loginCookie(t *testing.T, cfg AccountsConfig, handle, pw string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"handle":"`+handle+`","password":"`+pw+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoginHandler(cfg)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s status=%d body=%s", handle, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "kura_session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestSessionDiscoveryAnonymousAndLoginCookie(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	rec := httptest.NewRecorder()
	SessionHandler(cfg)(rec, req)
	var anon map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &anon); err != nil || rec.Code != 200 {
		t.Fatalf("anon session %d %v %s", rec.Code, err, rec.Body.String())
	}
	if anon["private_mode"] != true || anon["authenticated"] != false || anon["user"] != nil ||
		anon["can_read"] != false || anon["can_write"] != false || anon["can_moderate"] != false || anon["is_admin"] != false {
		t.Fatalf("anon shape=%v", anon)
	}

	// Login: editor gets write but not moderate; token only in cookie.
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"handle":"editor","password":"editor-password-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	LoginHandler(cfg)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "raw-token") {
		t.Fatal("raw session token leaked in JSON body")
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	user, _ := body["user"].(map[string]any)
	if body["can_write"] != true || body["can_moderate"] != false || user["handle"] != "editor" || user["role"] != "editor" {
		t.Fatalf("login body=%v", body)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "kura_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || !cookie.Secure || cookie.Path != "/" {
		t.Fatalf("cookie=%+v", cookie)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login response not no-store")
	}

	// Failed logins are generic and identical for unknown vs wrong password.
	bodies := []string{}
	for _, payload := range []string{`{"handle":"editor","password":"wrong-password-1"}`, `{"handle":"ghost","password":"editor-password-1"}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		LoginHandler(cfg)(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("bad login status=%d", w.Code)
		}
		bodies = append(bodies, w.Body.String())
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("login failure bodies differ (enumeration): %q vs %q", bodies[0], bodies[1])
	}

	// Form login is rejected; oversized bodies are rejected.
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("handle=editor&password=x"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	LoginHandler(cfg)(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form login status=%d", w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"handle":"`+strings.Repeat("a", 9000)+`","password":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	LoginHandler(cfg)(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized login status=%d", w.Code)
	}
	// Unknown JSON field is rejected.
	r = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"handle":"editor","password":"editor-password-1","admin":true}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	LoginHandler(cfg)(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("extra-field login status=%d", w.Code)
	}
}

func TestLogoutRevokesAndExpiresCookie(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	cookie := loginCookie(t, cfg, "viewer", "viewer-password-1")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	LogoutHandler(cfg)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status=%d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["authenticated"] != false || body["user"] != nil {
		t.Fatalf("logout body=%v", body)
	}
	cleared := rec.Result().Cookies()
	found := false
	for _, c := range cleared {
		if c.Name == "kura_session" && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("logout did not expire cookie")
	}
	if _, _, ok := stub.ResolveSession(context.Background(), cookie.Value); ok {
		t.Fatal("session still resolves after logout")
	}
}

func TestAdminUserEndpointsAndStrictPatch(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	admin := loginCookie(t, cfg, "admin", "admin-password-1")
	viewer := loginCookie(t, cfg, "viewer", "viewer-password-1")
	h := NewAccountsHandlers(cfg)

	do := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		switch {
		case method == http.MethodGet && path == "/api/auth/users":
			h.ListUsers(rec, req)
		case method == http.MethodPost:
			h.CreateUser(rec, req)
		default:
			// Route through chi so {id} is populated like production.
			r := chi.NewRouter()
			r.Patch("/api/auth/users/{id}", h.UpdateUser)
			r.ServeHTTP(rec, req)
			return rec
		}
		return rec
	}

	if rec := do(http.MethodGet, "/api/auth/users", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon list status=%d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/auth/users", "", viewer); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer list status=%d", rec.Code)
	}
	rec := do(http.MethodGet, "/api/auth/users", "", admin)
	var list struct {
		Users []userJSON `json:"users"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Users) != 3 {
		t.Fatalf("admin list %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/api/auth/users", `{"handle":"newbie","password":"newbie-password-1","role":"viewer"}`, admin)
	var created struct {
		User userJSON `json:"user"`
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &created) != nil || created.User.Handle != "newbie" {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") && !strings.Contains(rec.Body.String(), `"handle"`) {
		t.Fatal("unexpected body")
	}
	if rec := do(http.MethodPost, "/api/auth/users", `{"handle":"newbie","password":"newbie-password-1","role":"viewer"}`, admin); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/auth/users", `{"handle":"shorty","password":"short","role":"viewer"}`, admin); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "12 and 72") {
		t.Fatalf("short password status=%d %s", rec.Code, rec.Body.String())
	}

	id := json.Number("0")
	_ = id
	path := "/api/auth/users/4" // newbie is id 4 in the stub
	if rec := do(http.MethodPatch, path, `{"role":null}`, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("null patch status=%d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPatch, path, `{"role":"editor","admin":true}`, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("extra patch status=%d", rec.Code)
	}
	if rec := do(http.MethodPatch, path, `{}`, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty patch status=%d", rec.Code)
	}
	if rec := do(http.MethodPatch, "/api/auth/users/01", `{"role":"editor"}`, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-canonical id status=%d", rec.Code)
	}
	if rec := do(http.MethodPatch, "/api/auth/users/999", `{"role":"editor"}`, admin); rec.Code != http.StatusNotFound {
		t.Fatalf("missing user patch status=%d", rec.Code)
	}
	rec = do(http.MethodPatch, path, `{"role":"editor"}`, admin)
	var updated struct {
		User userJSON `json:"user"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &updated) != nil || updated.User.Role != "editor" {
		t.Fatalf("patch %d %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterAuthRoutesMountsFrozenPaths(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	r := chi.NewRouter()
	api := r.Route("/api", func(api chi.Router) { RegisterAuthRoutes(api, cfg) }).(chi.Router)
	_ = api
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"private_mode":true`) {
		t.Fatalf("mounted session %d %s", rec.Code, rec.Body.String())
	}
}
