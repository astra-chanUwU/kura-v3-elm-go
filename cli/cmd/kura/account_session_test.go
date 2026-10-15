package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runInputEnv(args []string, stdin string, env map[string]string) (int, string, string) {
	return runWithInputStrings(args, stdin, env)
}

func runWithInputStrings(args []string, stdin string, env map[string]string) (int, string, string) {
	var stdout, stderr strings.Builder
	code := runWithInput(args, &stdout, &stderr, strings.NewReader(stdin), func(k string) string { return env[k] })
	return code, stdout.String(), stderr.String()
}

type cliAccount struct {
	role  string
	token string
}

func newRoleServer(t *testing.T) (*httptest.Server, *roleState) {
	t.Helper()
	state := &roleState{live: map[string]cliAccount{
		"viewer": {role: "viewer", token: "viewer-session-token-1"},
		"editor": {role: "editor", token: "editor-session-token-1"},
		"admin":  {role: "admin", token: "admin-session-token-01"},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.requests++
		switch r.URL.Path {
		case "/api/auth/login":
			state.serveLogin(w, r)
		case "/api/auth/logout":
			state.serveLogout(w, r)
		case "/api/posts":
			state.serveRead(w, r)
		case "/api/posts/reactions":
			state.serveWrite(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, state
}

type roleState struct {
	requests int
	live     map[string]cliAccount
	revoked  map[string]bool
	echoAuth bool
}

func (s *roleState) serveLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Handle   string `json:"handle"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	account, ok := s.live[body.Handle]
	if !ok || body.Password != body.Handle+"-password-1" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid credentials"}`)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "kura_session", Value: account.token, Path: "/", HttpOnly: true,
		Expires: time.Now().Add(time.Hour), MaxAge: 3600,
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"authenticated":true,"user":{"id":1,"handle":"`+body.Handle+`","role":"`+account.role+`"}}`)
}

func (s *roleState) identity(r *http.Request) (string, bool) {
	if r.Header.Get("Authorization") == "Bearer deploy-token-value" {
		return "bearer", true
	}
	cookie, err := r.Cookie("kura_session")
	if err != nil || cookie.Value == "" || s.revoked[cookie.Value] {
		return "", false
	}
	for _, account := range s.live {
		if account.token == cookie.Value {
			return account.role, true
		}
	}
	return "", false
}

func (s *roleState) serveRead(w http.ResponseWriter, r *http.Request) {
	role, ok := s.identity(r)
	if !ok {
		s.deny(w, r)
		return
	}
	_ = role
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"posts":[]}`)
}

func (s *roleState) serveWrite(w http.ResponseWriter, r *http.Request) {
	role, ok := s.identity(r)
	if !ok {
		s.deny(w, r)
		return
	}
	if role == "viewer" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"write capability required"}`)
		return
	}
	if r.Header.Get("Authorization") == "" && r.Header.Get("Origin") == "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"untrusted origin"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"posts":[{"id":"7","version":1,"favorite":true,"score":0,"changed":true}]}`)
}

func (s *roleState) serveLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("kura_session"); err == nil {
		if s.revoked == nil {
			s.revoked = map[string]bool{}
		}
		s.revoked[cookie.Value] = true
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"authenticated":false}`)
}

func (s *roleState) deny(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusUnauthorized)
	msg := "authentication required"
	if s.echoAuth {
		msg = "rejected " + r.Header.Get("Authorization") + " " + r.Header.Get("Cookie")
	}
	_, _ = io.WriteString(w, `{"error":"`+msg+`"}`)
}

func sessionEnv(srv *httptest.Server, file string) map[string]string {
	return map[string]string{
		"KURA_API_URL":      srv.URL,
		"KURA_SESSION_FILE": file,
	}
}

func TestLoginPersistUsesRoleLimitedSession(t *testing.T) {
	srv, state := newRoleServer(t)
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "credentials.json")
	env := sessionEnv(srv, file)
	password := "viewer-password-1"

	code, out, errOut := runWithInputStrings([]string{"login", "--handle", "viewer", "--persist", "--json"}, password+"\n", env)
	if code != 0 {
		t.Fatalf("login code=%d stderr=%s", code, errOut)
	}
	if strings.Contains(out, password) || strings.Contains(errOut, password) || strings.Contains(out, "viewer-session-token-1") || strings.Contains(errOut, "viewer-session-token-1") {
		t.Fatal("login output contained a secret")
	}
	if !strings.Contains(out, `"role": "viewer"`) || !strings.Contains(out, `"persisted": true`) {
		t.Fatalf("login status=%s", out)
	}
	if err := assertUserPrivate(file); err != nil {
		t.Fatal(err)
	}
	if state.revoked["viewer-session-token-1"] {
		t.Fatal("persist revoked the session")
	}

	code, _, errOut = runWithInputStrings([]string{"search"}, "", env)
	if code != 0 || state.requests < 2 {
		t.Fatalf("viewer search code=%d requests=%d stderr=%s", code, state.requests, errOut)
	}
	code, _, errOut = runWithInputStrings([]string{"post", "favorite", "--version", "0", "--favorite=true", "7"}, "", env)
	if code == 0 || !strings.Contains(errOut, "write capability required") || strings.Contains(errOut, "viewer-session-token-1") {
		t.Fatalf("viewer write code=%d stderr=%s", code, errOut)
	}

	code, out, errOut = runWithInputStrings([]string{"login", "--handle", "editor", "--persist", "--json"}, "editor-password-1\n", env)
	if code != 0 || !strings.Contains(out, `"role": "editor"`) {
		t.Fatalf("editor login code=%d out=%s stderr=%s", code, out, errOut)
	}
	code, _, errOut = runWithInputStrings([]string{"post", "favorite", "--version", "0", "--favorite=true", "7"}, "", env)
	if code != 0 {
		t.Fatalf("editor write code=%d stderr=%s", code, errOut)
	}

	code, out, errOut = runWithInputStrings([]string{"login", "--handle", "admin", "--persist", "--json"}, "admin-password-1\n", env)
	if code != 0 || !strings.Contains(out, `"role": "admin"`) || strings.Contains(out, "admin-session-token-01") {
		t.Fatalf("admin login code=%d out=%s stderr=%s", code, out, errOut)
	}
	code, _, errOut = runWithInputStrings([]string{"post", "favorite", "--version", "0", "--favorite=true", "7"}, "", env)
	if code != 0 {
		t.Fatalf("admin write code=%d stderr=%s", code, errOut)
	}
}

func TestLogoutRevokesAndRemovesSession(t *testing.T) {
	srv, state := newRoleServer(t)
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "credentials.json")
	env := sessionEnv(srv, file)
	code, _, errOut := runWithInputStrings([]string{"login", "--handle", "viewer", "--persist"}, "viewer-password-1\n", env)
	if code != 0 {
		t.Fatalf("login stderr=%s", errOut)
	}
	code, out, errOut := runWithInputStrings([]string{"logout"}, "", env)
	if code != 0 || strings.TrimSpace(out) != "logged out" {
		t.Fatalf("logout code=%d out=%q stderr=%s", code, out, errOut)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("credential file still present: %v", err)
	}
	if !state.revoked["viewer-session-token-1"] {
		t.Fatal("server session was not revoked")
	}
	before := state.requests
	code, _, errOut = runWithInputStrings([]string{"search"}, "", env)
	if code == 0 || strings.Contains(errOut, "viewer-session-token-1") {
		t.Fatalf("post-logout search code=%d stderr=%s", code, errOut)
	}
	if state.requests != before+1 {
		t.Fatalf("search did not run anonymously after logout: before=%d after=%d", before, state.requests)
	}
}

func TestExpiredAndRevokedSessions(t *testing.T) {
	srv, state := newRoleServer(t)
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "credentials.json")
	env := sessionEnv(srv, file)
	expired := storedSession{
		Version: 1, APIURL: srv.URL, CookieName: "kura_session", Cookie: "viewer-session-token-1",
		ExpiresAt: time.Now().Add(-time.Hour), Handle: "viewer", Role: "viewer",
	}
	if err := writeStoredSession(file, expired); err != nil {
		t.Fatal(err)
	}
	before := state.requests
	code, _, errOut := runWithInputStrings([]string{"search"}, "", env)
	if code == 0 || !strings.Contains(errOut, "expired") || strings.Contains(errOut, "viewer-session-token-1") || state.requests != before {
		t.Fatalf("expired code=%d requests=%d stderr=%s", code, state.requests, errOut)
	}

	code, _, errOut = runWithInputStrings([]string{"login", "--handle", "viewer", "--persist"}, "viewer-password-1\n", env)
	if code != 0 {
		t.Fatalf("relogin stderr=%s", errOut)
	}
	state.revoked["viewer-session-token-1"] = true
	state.echoAuth = true
	code, _, errOut = runWithInputStrings([]string{"search"}, "", env)
	if code == 0 || strings.Contains(errOut, "viewer-session-token-1") || !strings.Contains(errOut, "[redacted]") {
		t.Fatalf("revoked error leaked session: code=%d stderr=%s", code, errOut)
	}
}

func TestMemorySessionAndNonInteractiveInput(t *testing.T) {
	srv, state := newRoleServer(t)
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "credentials.json")
	env := sessionEnv(srv, file)
	code, out, errOut := runWithInputStrings([]string{"login", "--handle", "editor", "--", "post", "favorite", "--version", "0", "--favorite=true", "7"}, "editor-password-1\n", env)
	if code != 0 {
		t.Fatalf("memory command code=%d out=%s stderr=%s", code, out, errOut)
	}
	if strings.Contains(out, "editor-password-1") || strings.Contains(errOut, "editor-password-1") || strings.Contains(out, "editor-session-token-1") || strings.Contains(errOut, "editor-session-token-1") {
		t.Fatal("memory command leaked a secret")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("memory login wrote a credential file")
	}
	if !state.revoked["editor-session-token-1"] {
		t.Fatal("memory session was not revoked on exit")
	}
}

func TestDeploymentTokenPrecedenceOverLogin(t *testing.T) {
	var auth, cookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		cookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"posts":[]}`)
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "credentials.json")
	if err := writeStoredSession(file, storedSession{
		Version: 1, APIURL: srv.URL, CookieName: "kura_session", Cookie: "viewer-session-token-1",
		ExpiresAt: time.Now().Add(time.Hour), Handle: "viewer", Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	env := sessionEnv(srv, file)
	env["KURA_API_TOKEN"] = "env-token-value"
	code, _, errOut := runWithInputStrings([]string{"search"}, "", env)
	if code != 0 || auth != "" || cookie != "" {
		t.Fatalf("env token on read code=%d auth=%q cookie=%q stderr=%s", code, auth, cookie, errOut)
	}
	code, _, errOut = runWithInputStrings([]string{"post", "favorite", "--version", "0", "--favorite=true", "7"}, "", env)
	if code != 0 || auth != "Bearer env-token-value" || cookie != "" || strings.Contains(errOut, "env-token-value") || strings.Contains(errOut, "viewer-session-token-1") {
		t.Fatalf("env precedence code=%d auth=%q cookie=%q stderr=%s", code, auth, cookie, errOut)
	}
	code, _, errOut = runWithInputStrings([]string{"post", "favorite", "--api-token", "flag-token-value", "--version", "0", "--favorite=true", "7"}, "", env)
	if code != 0 || auth != "Bearer flag-token-value" || cookie != "" {
		t.Fatalf("flag precedence code=%d auth=%q cookie=%q stderr=%s", code, auth, cookie, errOut)
	}
}

func TestPasswordRejectedAsFlagOrArgument(t *testing.T) {
	password := "viewer-password-1"
	code, _, errOut := runWithInputStrings([]string{"login", "--handle", "viewer", "--password=" + password}, "", nil)
	if code == 0 || strings.Contains(errOut, password) || !strings.Contains(errOut, "password cannot be passed as a flag") {
		t.Fatalf("flag code=%d stderr=%s", code, errOut)
	}
	code, _, errOut = runWithInputStrings([]string{"login", "--handle", "viewer", password}, "", nil)
	if code == 0 || strings.Contains(errOut, password) || !strings.Contains(errOut, "never from arguments") {
		t.Fatalf("arg code=%d stderr=%s", code, errOut)
	}
	code, out, _ := runWithInputStrings([]string{"login", "--help"}, "", nil)
	if code != 0 || !strings.Contains(out, "--persist") || !strings.Contains(out, "terminal") {
		t.Fatalf("login help code=%d", code)
	}
	code, out, _ = runWithInputStrings([]string{"--help"}, "", nil)
	if code != 0 || !strings.Contains(out, "--api-token") || !strings.Contains(out, "KURA_API_TOKEN") || !strings.Contains(out, "logged-in account") {
		t.Fatalf("root help missing precedence")
	}
}

func TestTrustedOriginEnvIsSent(t *testing.T) {
	var origin, cookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			http.SetCookie(w, &http.Cookie{
				Name: "kura_session", Value: "editor-session-token-1", Path: "/",
				Expires: time.Now().Add(time.Hour), MaxAge: 3600,
			})
			_, _ = io.WriteString(w, `{"authenticated":true,"user":{"id":2,"handle":"editor","role":"editor"}}`)
			return
		}
		origin = r.Header.Get("Origin")
		cookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"posts":[{"id":"7","version":1,"favorite":true,"score":0,"changed":true}]}`)
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "credentials.json")
	env := sessionEnv(srv, file)
	env["KURA_AUTH_TRUSTED_ORIGIN"] = "https://library.example"
	code, _, errOut := runWithInputStrings([]string{"login", "--handle", "editor", "--", "post", "favorite", "--version", "0", "--favorite=true", "7"}, "editor-password-1\n", env)
	if code != 0 || origin != "https://library.example" || cookie != "kura_session=editor-session-token-1" {
		t.Fatalf("code=%d origin=%q cookie=%q stderr=%s", code, origin, cookie, errOut)
	}
}
