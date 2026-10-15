package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccountSessionSendsCookieNotBearer(t *testing.T) {
	var gotAuth, gotCookie, gotOrigin string
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = ""
	c.SessionCookieName = "kura_session"
	c.SessionCookieValue = "viewer-session-token-1"
	if _, err := c.Search(context.Background(), "cat"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || gotAuth != "" || gotCookie != "kura_session=viewer-session-token-1" || gotOrigin != "" {
		t.Fatalf("GET auth=%q cookie=%q origin=%q", gotAuth, gotCookie, gotOrigin)
	}
	if _, err := c.EditReactions(context.Background(), ReactionRequest{}); err != nil {
		t.Fatal(err)
	}
	origin := strings.TrimPrefix(srv.URL, "http://")
	if gotAuth != "" || gotCookie != "kura_session=viewer-session-token-1" || gotOrigin != "http://"+origin {
		t.Fatalf("POST auth=%q cookie=%q origin=%q", gotAuth, gotCookie, gotOrigin)
	}
}

func TestDeploymentTokenPrecedesAccountSession(t *testing.T) {
	var gotAuth, gotCookie, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"7","version":1,"favorite":true,"score":0,"changed":true}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "deploy-token-value"
	c.SessionCookieName = "kura_session"
	c.SessionCookieValue = "viewer-session-token-1"
	if _, err := c.EditReactions(context.Background(), ReactionRequest{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer deploy-token-value" || gotCookie != "" || gotOrigin != "" {
		t.Fatalf("auth=%q cookie=%q origin=%q", gotAuth, gotCookie, gotOrigin)
	}
}

func TestTrustedOriginOverridesDerivedOrigin(t *testing.T) {
	var gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"7","version":1,"favorite":true,"score":0,"changed":true}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.APIToken = ""
	c.SessionCookieName = "kura_session"
	c.SessionCookieValue = "viewer-session-token-1"
	c.Origin = "https://library.example"
	if _, err := c.EditReactions(context.Background(), ReactionRequest{}); err != nil {
		t.Fatal(err)
	}
	if gotOrigin != "https://library.example" {
		t.Fatalf("origin=%q", gotOrigin)
	}
}

func TestErrorsRedactAuthorizationAndCookie(t *testing.T) {
	const token = "deploy-token-value"
	const cookie = "viewer-session-token-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"rejected Bearer `+token+` and `+cookie+`"}`)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = token
	c.SessionCookieName = "kura_session"
	c.SessionCookieValue = cookie
	_, err := c.EditReactions(context.Background(), ReactionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if strings.Contains(text, token) || strings.Contains(text, cookie) || !strings.Contains(text, "[redacted]") {
		t.Fatalf("error leaked session material: %s", text)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("error type %T %v", err, err)
	}

	c.APIToken = token
	c.SessionCookieValue = ""
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed with Bearer " + token + " and Cookie kura_session=" + cookie)
	})}
	_, err = c.EditReactions(context.Background(), ReactionRequest{})
	if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("transport error leaked token: %v", err)
	}
	var reqErr *RequestError
	if !errors.As(err, &reqErr) || !reqErr.Uncertain {
		t.Fatalf("transport error lost uncertain write: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoginCapturesCookieAndRedactsPassword(t *testing.T) {
	const password = "viewer-password-1"
	const cookie = "viewer-session-token-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("login sent authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), password) {
			t.Errorf("server did not receive password")
		}
		http.SetCookie(w, &http.Cookie{
			Name: "kura_session", Value: cookie, Path: "/", HttpOnly: true,
			Expires: time.Now().Add(time.Hour), MaxAge: 3600,
		})
		_, _ = w.Write([]byte(`{"authenticated":true,"user":{"id":3,"handle":"viewer","role":"viewer"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.Login(context.Background(), "viewer", password)
	if err != nil {
		t.Fatal(err)
	}
	if got.Handle != "viewer" || got.Role != "viewer" || got.CookieName != "kura_session" || got.CookieValue != cookie || !got.ExpiresAt.After(time.Now()) {
		t.Fatalf("login result handle=%s role=%s cookie=%s expiry=%s", got.Handle, got.Role, got.CookieName, got.ExpiresAt)
	}

	leak := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad password `+password+`"}`)
	}))
	defer leak.Close()
	c = New(leak.URL)
	_, err = c.Login(context.Background(), "viewer", password)
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("login error leaked password: %v", err)
	}
}

func TestLoginRejectsTokenInBody(t *testing.T) {
	const cookie = "viewer-session-token-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name: "kura_session", Value: cookie, Path: "/",
			Expires: time.Now().Add(time.Hour), MaxAge: 3600,
		})
		_, _ = io.WriteString(w, `{"authenticated":true,"user":{"handle":"viewer","role":"viewer"},"token":"`+cookie+`"}`)
	}))
	defer srv.Close()
	_, err := New(srv.URL).Login(context.Background(), "viewer", "viewer-password-1")
	if err == nil || strings.Contains(err.Error(), cookie) {
		t.Fatalf("body token leak: %v", err)
	}
}
