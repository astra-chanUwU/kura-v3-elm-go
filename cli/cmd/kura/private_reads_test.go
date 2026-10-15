package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateReadsSearchServer(t *testing.T, auth *string, requests *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		*auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[{"id":"post-123","preview_url":"/p.jpg","original_url":"/o.jpg","media_type":"image/jpeg","width":10,"height":10}],"next_cursor":null}`)
	}))
}

func makePrivateReadsZIP(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("originals/1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrivateReadsValidationFailsBeforeHTTP(t *testing.T) {
	for _, value := range []string{"yes", "2", "maybe", "on"} {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			fmt.Fprint(w, `{"posts":[]}`)
		}))
		code, _, errOut := runWithEnv([]string{"search", "cat"}, map[string]string{
			"KURA_API_URL":       srv.URL,
			"KURA_API_TOKEN":     "supplied-token",
			"KURA_PRIVATE_READS": value,
		})
		srv.Close()
		if code == 0 || requests != 0 || !strings.Contains(errOut, "KURA_PRIVATE_READS") {
			t.Fatalf("value %q code=%d requests=%d stderr=%q", value, code, requests, errOut)
		}
	}
}

func TestPrivateReadsOptInWithoutTokenFailsBeforeHTTP(t *testing.T) {
	for _, value := range []string{"true", "1", "TRUE", "True"} {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			fmt.Fprint(w, `{"posts":[]}`)
		}))
		code, _, errOut := runWithEnv([]string{"search", "cat"}, map[string]string{
			"KURA_API_URL":       srv.URL,
			"KURA_PRIVATE_READS": value,
		})
		srv.Close()
		if code == 0 || requests != 0 || !strings.Contains(errOut, "KURA_API_TOKEN") {
			t.Fatalf("value %q code=%d requests=%d stderr=%q", value, code, requests, errOut)
		}
	}
}

func TestPrivateReadsExplicitFalseNeedsNoToken(t *testing.T) {
	for _, value := range []string{"false", "0", "FALSE", "False", ""} {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"posts":[]}`)
		}))
		env := map[string]string{"KURA_API_URL": srv.URL}
		if value != "" {
			env["KURA_PRIVATE_READS"] = value
		}
		c, err := newAPIClient(context.Background(), srv.URL, "", func(k string) string { return env[k] })
		if err != nil {
			srv.Close()
			t.Fatalf("value %q unexpected error: %v", value, err)
		}
		if c.PrivateReads {
			srv.Close()
			t.Fatalf("value %q must leave PrivateReads false", value)
		}
		code, _, _ := runWithEnv([]string{"search", "empty"}, env)
		srv.Close()
		if code != 0 || requests != 1 {
			t.Fatalf("value %q code=%d requests=%d", value, code, requests)
		}
	}
}

func TestPrivateReadsVariantsOptInWithToken(t *testing.T) {
	for _, value := range []string{"true", "1", "TRUE", "True"} {
		c, err := newAPIClient(context.Background(), "http://localhost:8080", "", func(k string) string {
			switch k {
			case "KURA_API_TOKEN":
				return "supplied-token"
			case "KURA_PRIVATE_READS":
				return value
			}
			return ""
		})
		if err != nil || !c.PrivateReads || c.APIToken != "supplied-token" {
			t.Fatalf("value %q client=%+v err=%v", value, c, err)
		}
	}
}

func TestCustomGetenvCannotLeakAmbientPrivateReads(t *testing.T) {
	t.Setenv("KURA_API_TOKEN", "ambient-token-must-not-be-used")
	t.Setenv("KURA_PRIVATE_READS", "true")
	c, err := newAPIClient(context.Background(), "http://localhost:8080", "", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.APIToken != "" || c.PrivateReads {
		t.Fatalf("ambient environment leaked: %+v", c)
	}
}

func TestCLISearchPrivateReadsAuthBehavior(t *testing.T) {
	var auth string
	requests := 0
	srv := privateReadsSearchServer(t, &auth, &requests)
	defer srv.Close()

	// Default: token present but no opt-in sends nothing.
	code, _, _ := runWithEnv([]string{"search", "cat"}, map[string]string{
		"KURA_API_URL":   srv.URL,
		"KURA_API_TOKEN": "supplied-token",
	})
	if code != 0 || requests != 1 || auth != "" {
		t.Fatalf("default code=%d requests=%d auth=%q", code, requests, auth)
	}
	// Opt-in: same token is sent on the library GET.
	code, _, _ = runWithEnv([]string{"search", "cat"}, map[string]string{
		"KURA_API_URL":       srv.URL,
		"KURA_API_TOKEN":     "supplied-token",
		"KURA_PRIVATE_READS": "true",
	})
	if code != 0 || requests != 2 || auth != "Bearer supplied-token" {
		t.Fatalf("opt-in code=%d requests=%d auth=%q", code, requests, auth)
	}
	// Numeric 1 variant also opts in.
	code, _, _ = runWithEnv([]string{"search", "cat"}, map[string]string{
		"KURA_API_URL":       srv.URL,
		"KURA_API_TOKEN":     "supplied-token",
		"KURA_PRIVATE_READS": "1",
	})
	if code != 0 || auth != "Bearer supplied-token" {
		t.Fatalf("numeric opt-in auth=%q", auth)
	}
}

func TestCLIExportPrivateReadsAuthBehavior(t *testing.T) {
	archive := makePrivateReadsZIP(t)
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	code, _, errOut := runWithEnv([]string{"export", "--ids", "1", "--output", filepath.Join(dir, "default.zip")}, map[string]string{
		"KURA_API_URL":   srv.URL,
		"KURA_API_TOKEN": "supplied-token",
	})
	if code != 0 || auth != "" {
		t.Fatalf("default ZIP code=%d auth=%q stderr=%q", code, auth, errOut)
	}
	code, _, errOut = runWithEnv([]string{"export", "--ids", "1", "--output", filepath.Join(dir, "private.zip")}, map[string]string{
		"KURA_API_URL":       srv.URL,
		"KURA_API_TOKEN":     "supplied-token",
		"KURA_PRIVATE_READS": "true",
	})
	if code != 0 || auth != "Bearer supplied-token" {
		t.Fatalf("opt-in ZIP code=%d auth=%q stderr=%q", code, auth, errOut)
	}
}

func TestCLIOffOriginRedirectNeverLeaksCredentials(t *testing.T) {
	evilHits := 0
	var evilAuth []string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		evilAuth = append(evilAuth, r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"posts":[]}`)
	}))
	defer evil.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/copy", http.StatusFound)
	}))
	defer api.Close()

	code, _, _ := runWithEnv([]string{"search", "cat"}, map[string]string{
		"KURA_API_URL":       api.URL,
		"KURA_API_TOKEN":     "supplied-token",
		"KURA_PRIVATE_READS": "true",
	})
	if code == 0 || evilHits != 0 {
		t.Fatalf("JSON redirect not fail-closed: code=%d evilHits=%d auth=%v", code, evilHits, evilAuth)
	}
}

func TestCLIZIPOffOriginRedirectNeverLeaksCredentials(t *testing.T) {
	evilHits := 0
	var evilAuth []string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		evilAuth = append(evilAuth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(makePrivateReadsZIP(t))
	}))
	defer evil.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/copy.zip", http.StatusFound)
	}))
	defer api.Close()

	out := filepath.Join(t.TempDir(), "out.zip")
	code, _, _ := runWithEnv([]string{"export", "--ids", "1", "--output", out}, map[string]string{
		"KURA_API_URL":       api.URL,
		"KURA_API_TOKEN":     "supplied-token",
		"KURA_PRIVATE_READS": "true",
	})
	if code == 0 || evilHits != 0 {
		t.Fatalf("ZIP redirect not fail-closed: code=%d evilHits=%d auth=%v", code, evilHits, evilAuth)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("redirect must not publish a file: stat=%v", err)
	}
}

func TestCLIHelpDocumentsPrivateReads(t *testing.T) {
	code, out, _ := runWithEnv([]string{"--help"}, nil)
	if code != 0 || !strings.Contains(out, "KURA_PRIVATE_READS") {
		t.Fatalf("root help must document KURA_PRIVATE_READS: code=%d out=%q", code, out)
	}
}
