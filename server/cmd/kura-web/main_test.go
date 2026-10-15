package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyPreservesSameOriginRequestAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.RequestURI(); got != "/api/auth/session?next=%2Fgrid&x=1" {
			t.Errorf("request URI=%q", got)
		}
		if r.Host != "localhost:8000" {
			t.Errorf("Host=%q, want original browser host", r.Host)
		}
		if r.Header.Get("Origin") != "http://localhost:8000" {
			t.Errorf("Origin=%q", r.Header.Get("Origin"))
		}
		if r.Header.Get("Cookie") != "kura_session=opaque" {
			t.Errorf("Cookie=%q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("X-Forwarded-Host") != "" || r.Header.Get("Forwarded") != "" {
			t.Errorf("untrusted forwarded headers reached backend")
		}
		w.Header().Add("Set-Cookie", "kura_session=next; HttpOnly; SameSite=Strict; Path=/")
		w.Header().Set("Location", "/api/auth/session")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	handler, err := newWebHandler(t.TempDir(), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost:8000/api/auth/session?next=%2Fgrid&x=1", nil)
	request.Header.Set("Origin", "http://localhost:8000")
	request.AddCookie(&http.Cookie{Name: "kura_session", Value: "opaque"})
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	request.Header.Set("Forwarded", "host=attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/api/auth/session" {
		t.Fatalf("Location=%q", response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "kura_session" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("Set-Cookie did not pass through: %#v", cookies)
	}
}

func TestProxyForwardsMediaAndHealthPaths(t *testing.T) {
	var received []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Method+" "+r.URL.RequestURI())
		if r.URL.Path == "/media/uploads/photo.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "image bytes")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "healthy")
	}))
	defer upstream.Close()
	handler, err := newWebHandler(t.TempDir(), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/media/uploads/photo.jpg", "/health"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
	if strings.Join(received, ",") != "GET /media/uploads/photo.jpg,GET /health" {
		t.Fatalf("forwarded paths=%v", received)
	}
}

func TestProxyUnavailableIsGenericAndStaticStillWorks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	target := upstream.URL
	upstream.Close()

	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("Kura UI"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebHandler(staticDir, target)
	if err != nil {
		t.Fatal(err)
	}
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	if apiResponse.Code != http.StatusBadGateway || apiResponse.Body.String() != "API unavailable\n" || strings.Contains(apiResponse.Body.String(), target) {
		t.Fatalf("API failure response=%d %q", apiResponse.Code, apiResponse.Body.String())
	}
	staticResponse := httptest.NewRecorder()
	handler.ServeHTTP(staticResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if staticResponse.Code != http.StatusOK || staticResponse.Body.String() != "Kura UI" {
		t.Fatalf("static response=%d %q", staticResponse.Code, staticResponse.Body.String())
	}
}

func TestValidateProxyTargetAndSelfLoop(t *testing.T) {
	valid, err := validateProxyTarget("")
	if err != nil || valid.String() != defaultAPITarget {
		t.Fatalf("default target=%v err=%v", valid, err)
	}
	for _, invalid := range []string{
		"/api",
		"ftp://127.0.0.1:8080",
		"http://user:password@127.0.0.1:8080",
		"http://127.0.0.1:8080?token=x",
		"http://127.0.0.1:8080#fragment",
		"http://127.0.0.1:not-a-port",
		"http://localhost:8000",
		"http://127.0.0.1:8000",
		"http://0.0.0.0:8000",
		"http://[::1]:8000",
	} {
		t.Run(invalid, func(t *testing.T) {
			if _, err := validateProxyTarget(invalid); err == nil {
				t.Fatalf("accepted invalid target %q", invalid)
			}
		})
	}
	if _, err := validateProxyTarget("https://kura-api.example:8443/base"); err != nil {
		t.Fatalf("valid remote API target rejected: %v", err)
	}
	parsed, _ := url.Parse("http://127.0.0.1:8080")
	if isAPIProxyPath("/api/auth/session") == false || isAPIProxyPath("/media/a.jpg") == false || isAPIProxyPath("/health") == false || isAPIProxyPath("/index.html") {
		t.Fatal("API routing boundary is incorrect")
	}
	if proxyLoopsToWeb(parsed) {
		t.Fatal("API target on port 8080 incorrectly treated as frontend loop")
	}
}
