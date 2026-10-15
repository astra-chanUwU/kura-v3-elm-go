package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCLIShapedSessionUsesExistingLogin proves a non-browser client can
// issue, use, and revoke an account session through the existing login
// cookie. It does not add a second credential type.
func TestCLIShapedSessionUsesExistingLogin(t *testing.T) {
	stub := newStubAuth()
	accounts := NewAccountsConfig(stub)
	accounts.PrivateMode = true
	accounts.AllowHTTPLocal = true
	accounts.DeploymentToken = "deploy-secret"
	access := accounts.access()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", LoginHandler(accounts))
	mux.HandleFunc("POST /api/auth/logout", LogoutHandler(accounts))
	mux.Handle("GET /api/posts", access.Wrap(downstream()))
	mux.Handle("POST /api/collections", access.Wrap(downstream()))
	mux.Handle("GET /api/auth/users", access.Wrap(downstream()))
	mux.Handle("GET /api/posts/moderation", access.Wrap(downstream()))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	login := func(handle, password string) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", strings.NewReader(`{"handle":"`+handle+`","password":"`+password+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login %s status=%d body=%s", handle, resp.StatusCode, body)
		}
		var token string
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "kura_session" {
				token = cookie.Value
			}
		}
		if token == "" || strings.Contains(string(body), token) || strings.Contains(string(body), password) {
			t.Fatalf("login response leaked session material for %s", handle)
		}
		if !strings.Contains(string(body), `"role":"`+handle+`"`) && handle != "admin" && handle != "editor" && handle != "viewer" {
			t.Fatalf("login body %s", body)
		}
		if !strings.Contains(string(body), `"role":"`+roleOf(handle)+`"`) {
			t.Fatalf("login body %s", body)
		}
		return token
	}
	call := func(method, path, token string, origin bool, bearer string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Cookie", "kura_session="+token)
		}
		if origin {
			req.Header.Set("Origin", srv.URL)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	status := func(method, path, token string, origin bool, bearer string) (int, string) {
		t.Helper()
		resp := call(method, path, token, origin, bearer)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header.Get("X-Actor")
	}

	viewer := login("viewer", "viewer-password-1")
	editor := login("editor", "editor-password-1")
	admin := login("admin", "admin-password-1")

	if code, actor := status(http.MethodGet, "/api/posts", viewer, false, ""); code != http.StatusOK || actor != "user:3" {
		t.Fatalf("viewer read %d actor=%s", code, actor)
	}
	if code, _ := status(http.MethodPost, "/api/collections", viewer, true, ""); code != http.StatusForbidden {
		t.Fatalf("viewer write %d", code)
	}
	if code, _ := status(http.MethodPost, "/api/collections", editor, false, ""); code != http.StatusForbidden {
		t.Fatalf("editor write without origin %d", code)
	}
	if code, _ := status(http.MethodPost, "/api/collections", editor, true, ""); code != http.StatusOK {
		t.Fatalf("editor write %d", code)
	}
	if code, _ := status(http.MethodGet, "/api/auth/users", editor, false, ""); code != http.StatusForbidden {
		t.Fatalf("editor admin %d", code)
	}
	if code, _ := status(http.MethodGet, "/api/posts/moderation", editor, false, ""); code != http.StatusForbidden {
		t.Fatalf("editor moderation %d", code)
	}
	if code, _ := status(http.MethodGet, "/api/auth/users", admin, false, ""); code != http.StatusOK {
		t.Fatalf("admin users %d", code)
	}
	if code, _ := status(http.MethodGet, "/api/posts/moderation", admin, false, ""); code != http.StatusOK {
		t.Fatalf("admin moderation %d", code)
	}
	if code, actor := status(http.MethodPost, "/api/collections", viewer, false, "deploy-secret"); code != http.StatusOK || actor != "system" {
		t.Fatalf("deployment token precedence %d actor=%s", code, actor)
	}

	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	logoutReq.Header.Set("Cookie", "kura_session="+viewer)
	logoutResp, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout %d", logoutResp.StatusCode)
	}
	if code, _ := status(http.MethodGet, "/api/posts", viewer, false, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked viewer %d", code)
	}

	stub.expired[editor] = true
	if code, _ := status(http.MethodGet, "/api/posts", editor, false, ""); code != http.StatusUnauthorized {
		t.Fatalf("expired editor %d", code)
	}
}

func roleOf(handle string) string { return handle }
