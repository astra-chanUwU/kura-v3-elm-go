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

func (s *stubAuth) ChangePassword(_ context.Context, actor auth.Actor, current, next string) error {
	if actor.UserID <= 0 {
		return auth.ErrForbidden
	}
	if err := auth.ValidatePassword(next); err != nil {
		return err
	}
	pw, ok := s.password[actor.UserID]
	if !ok || pw != current {
		return auth.ErrInvalidCredentials
	}
	s.password[actor.UserID] = next
	for raw, sess := range s.sessions {
		if sess.ID == actor.UserID {
			delete(s.sessions, raw)
		}
	}
	return nil
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

func passwordChangeReq(cookie *http.Cookie, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://kura.example")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func TestChangePasswordSelfService(t *testing.T) {
	stub := newStubAuth()
	cfg := testAccountsConfig(stub)
	cfg.DeploymentToken = "deploy-secret"
	viewer := loginCookie(t, cfg, "viewer", "viewer-password-1")
	other := loginCookie(t, cfg, "viewer", "viewer-password-1")
	admin := loginCookie(t, cfg, "admin", "admin-password-1")
	editor := loginCookie(t, cfg, "editor", "editor-password-1")
	handler := ChangePasswordHandler(cfg)

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	if rec := serve(passwordChangeReq(nil, `{"current_password":"viewer-password-1","new_password":"viewer-password-2"}`)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d %s", rec.Code, rec.Body.String())
	}
	bearer := passwordChangeReq(nil, `{"current_password":"viewer-password-1","new_password":"viewer-password-2","user_id":3}`)
	bearer.Header.Set("Authorization", "Bearer deploy-secret")
	if rec := serve(bearer); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bearer status=%d %s", rec.Code, rec.Body.String())
	}
	if stub.password[3] != "viewer-password-1" || stub.password[1] != "admin-password-1" {
		t.Fatalf("bearer changed passwords: %#v", stub.password)
	}

	targeted := passwordChangeReq(viewer, `{"current_password":"viewer-password-1","new_password":"viewer-password-2","user_id":1}`)
	if rec := serve(targeted); rec.Code != http.StatusBadRequest {
		t.Fatalf("targeted body status=%d %s", rec.Code, rec.Body.String())
	}
	if stub.password[1] != "admin-password-1" || stub.password[3] != "viewer-password-1" {
		t.Fatalf("targeted change applied: %#v", stub.password)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/auth/users/1", strings.NewReader(`{"password":"admin-password-2"}`))
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("Origin", "https://kura.example")
	patch.AddCookie(viewer)
	patchRec := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Patch("/api/auth/users/{id}", UpdateUserHandler(cfg))
	r.ServeHTTP(patchRec, patch)
	if patchRec.Code != http.StatusForbidden || stub.password[1] != "admin-password-1" {
		t.Fatalf("viewer admin patch status=%d passwords=%#v", patchRec.Code, stub.password)
	}

	wrong := passwordChangeReq(viewer, `{"current_password":"wrong-password-1","new_password":"viewer-password-2"}`)
	if rec := serve(wrong); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "current password") {
		t.Fatalf("wrong current status=%d %s", rec.Code, rec.Body.String())
	}
	if _, _, ok := stub.ResolveSession(context.Background(), viewer.Value); !ok {
		t.Fatal("wrong current password revoked the session")
	}
	short := passwordChangeReq(viewer, `{"current_password":"viewer-password-1","new_password":"short"}`)
	if rec := serve(short); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "12 and 72") {
		t.Fatalf("short password status=%d %s", rec.Code, rec.Body.String())
	}

	untrusted := passwordChangeReq(viewer, `{"current_password":"viewer-password-1","new_password":"viewer-password-2"}`)
	untrusted.Header.Set("Origin", "https://evil.example")
	if rec := serve(untrusted); rec.Code != http.StatusForbidden || stub.password[3] != "viewer-password-1" {
		t.Fatalf("untrusted origin status=%d %s", rec.Code, rec.Body.String())
	}

	okReq := passwordChangeReq(viewer, `{"current_password":"viewer-password-1","new_password":"viewer-password-2"}`)
	rec := serve(okReq)
	var body map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["authenticated"] != false || body["user"] != nil {
		t.Fatalf("change status=%d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "viewer-password") {
		t.Fatal("password echoed in response")
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "kura_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("password change did not clear the session cookie")
	}
	if _, _, ok := stub.ResolveSession(context.Background(), viewer.Value); ok {
		t.Fatal("changed account session still resolves")
	}
	if _, _, ok := stub.ResolveSession(context.Background(), other.Value); ok {
		t.Fatal("second session for the same account still resolves")
	}
	if _, _, ok := stub.ResolveSession(context.Background(), admin.Value); !ok {
		t.Fatal("admin session was revoked")
	}
	if stub.password[3] != "viewer-password-2" || stub.password[1] != "admin-password-1" || stub.password[2] != "editor-password-1" {
		t.Fatalf("passwords=%#v", stub.password)
	}
	if _, err := stub.Login(context.Background(), "viewer", "viewer-password-1"); err == nil {
		t.Fatal("old viewer password still works")
	}
	if _, err := stub.Login(context.Background(), "viewer", "viewer-password-2"); err != nil {
		t.Fatal(err)
	}

	editorReq := passwordChangeReq(editor, `{"current_password":"editor-password-1","new_password":"editor-password-2"}`)
	if rec := serve(editorReq); rec.Code != http.StatusOK || stub.password[2] != "editor-password-2" {
		t.Fatalf("editor change status=%d passwords=%#v body=%s", rec.Code, stub.password, rec.Body.String())
	}
	adminReq := passwordChangeReq(admin, `{"current_password":"admin-password-1","new_password":"admin-password-2"}`)
	if rec := serve(adminReq); rec.Code != http.StatusOK || stub.password[1] != "admin-password-2" {
		t.Fatalf("admin change status=%d passwords=%#v body=%s", rec.Code, stub.password, rec.Body.String())
	}

	stub.expired[admin.Value] = true
	expired := passwordChangeReq(admin, `{"current_password":"admin-password-2","new_password":"admin-password-3"}`)
	if rec := serve(expired); rec.Code != http.StatusUnauthorized || stub.password[1] != "admin-password-2" {
		t.Fatalf("expired status=%d %s", rec.Code, rec.Body.String())
	}
}

func TestViewerPasswordChangeBypassesWriteToken(t *testing.T) {
	stub := newStubAuth()
	cfg := NewAccountsConfig(stub)
	cfg.TrustedOrigin = "https://kura.example"
	router := NewRouterWithDependencies("deploy-secret", Dependencies{Accounts: &cfg})
	cookie := wiringLogin(t, router, "viewer", "viewer-password-1")

	blocked := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{}`))
	blocked.Header.Set("Content-Type", "application/json")
	blocked.AddCookie(cookie)
	if rec := wiringServe(router, blocked); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "write capability required") {
		t.Fatalf("viewer library write status=%d %s", rec.Code, rec.Body.String())
	}

	missing := passwordChangeReq(nil, `{"current_password":"viewer-password-1","new_password":"viewer-password-2"}`)
	if rec := wiringServe(router, missing); rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "write capability required") {
		t.Fatalf("anonymous password status=%d %s", rec.Code, rec.Body.String())
	}
	changed := passwordChangeReq(cookie, `{"current_password":"viewer-password-1","new_password":"viewer-password-2"}`)
	rec := wiringServe(router, changed)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authenticated":false`) || stub.password[3] != "viewer-password-2" {
		t.Fatalf("viewer password status=%d %s passwords=%#v", rec.Code, rec.Body.String(), stub.password)
	}
	if stub.password[1] != "admin-password-1" {
		t.Fatal("another account changed")
	}
}
