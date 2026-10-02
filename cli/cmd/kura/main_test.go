package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/posts", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		switch q {
		case "":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"posts":[]}`)
		case "cat":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"posts":[{"id":"post-123","preview_url":"/media/post-123/preview.jpg","original_url":"/media/post-123/original.jpg","media_type":"image/jpeg","width":640,"height":480}],"next_cursor":"tok123"}`)
		case "empty":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"posts":[]}`)
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"search query is too long (maximum 256 characters)"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"posts":[{"id":"post-999","preview_url":"/p.jpg","original_url":"/o.jpg","media_type":"image/png","width":10,"height":20}]}`)
		}
	})
	return httptest.NewServer(mux)
}

func runWithEnv(args []string, env map[string]string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	getenv := func(k string) string { return env[k] }
	code := run(args, &stdout, &stderr, getenv)
	return code, stdout.String(), stderr.String()
}

func TestSearch_HumanOutput(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"search", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if !strings.Contains(out, "post-123") || !strings.Contains(out, "image/jpeg") {
		t.Fatalf("human output missing: %q", out)
	}
	if !strings.Contains(errOut, "tok123") {
		t.Fatalf("expected next_cursor hint on stderr, got %q", errOut)
	}
}

func TestSearch_JSONOutput(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, out, _ := runWithEnv([]string{"search", "--json", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, `"posts"`) || !strings.Contains(out, "post-123") || !strings.Contains(out, "next_cursor") {
		t.Fatalf("json output missing: %q", out)
	}
}

func TestSearch_JSONLOutput(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"search", "--jsonl", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 jsonl line, got %q", out)
	}
	if !strings.Contains(lines[0], `"id":"post-123"`) {
		t.Fatalf("jsonl line missing: %q", lines[0])
	}
	if !strings.Contains(errOut, "tok123") {
		t.Fatalf("expected cursor hint, got %q", errOut)
	}
}

func TestSearch_EmptyBrowse_NoOutput(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"search"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if out != "" {
		t.Fatalf("expected empty stdout for browse, got %q", out)
	}
	// empty --json should produce envelope
	code, out, _ = runWithEnv([]string{"search", "--json"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, `"posts"`) {
		t.Fatalf("expected json posts, got %q", out)
	}
	// --jsonl empty should produce no stdout
	code, out, _ = runWithEnv([]string{"search", "--jsonl"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out != "" {
		t.Fatalf("expected empty jsonl, got %q", out)
	}
}

func TestSearch_APIErrorReadable(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"search", "--json", "bad"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	if out != "" {
		t.Fatalf("expected empty stdout on error, got %q", out)
	}
	if !strings.Contains(errOut, "too long") {
		t.Fatalf("expected readable error, got %q", errOut)
	}
}

func TestSearch_UnsupportedLimitAndCursor(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, _, errOut := runWithEnv([]string{"search", "--limit", "10", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected non-zero for --limit")
	}
	if !strings.Contains(errOut, "--limit") || !strings.Contains(errOut, "not supported") {
		t.Fatalf("expected limit unsupported message, got %q", errOut)
	}

	code, _, errOut = runWithEnv([]string{"search", "--cursor", "abc", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected non-zero for --cursor")
	}
	if !strings.Contains(errOut, "--cursor") || !strings.Contains(errOut, "not supported") {
		t.Fatalf("expected cursor unsupported message, got %q", errOut)
	}
}

func TestSearch_FlagsBeforeQuery(t *testing.T) {
	// flags after query are treated as part of query, not as flags
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[]}`)
	}))
	defer srv.Close()

	code, _, _ := runWithEnv([]string{"search", "cat", "--json"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if gotQ != "cat --json" {
		t.Fatalf("expected query %q, got %q", "cat --json", gotQ)
	}

	// --json before query should be a flag, not part of query
	gotQ = ""
	code, out, _ := runWithEnv([]string{"search", "--json", "cat", "--json"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if gotQ != "cat --json" {
		t.Fatalf("expected query with --json part, got %q", gotQ)
	}
	if !strings.Contains(out, `"posts"`) {
		t.Fatalf("expected json output, got %q", out)
	}
}

func TestSearch_URL_Encoding(t *testing.T) {
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRaw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[]}`)
	}))
	defer srv.Close()

	_, _, _ = runWithEnv([]string{"search", "a&b=c d"}, map[string]string{"KURA_API_URL": srv.URL})
	if gotRaw != "q=a%26b%3Dc+d" {
		t.Fatalf("encoding mismatch: got %q want %q", gotRaw, "q=a%26b%3Dc+d")
	}
}

func TestSearch_UnknownFlag(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, _, errOut := runWithEnv([]string{"search", "--unknown", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected non-zero for unknown flag")
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("expected unknown flag message, got %q", errOut)
	}
}

func TestSearch_MutuallyExclusiveJSON(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, _, errOut := runWithEnv([]string{"search", "--json", "--jsonl", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected non-zero for mutually exclusive flags")
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Fatalf("expected exclusive message, got %q", errOut)
	}
}

func TestSearch_Help(t *testing.T) {
	code, out, _ := runWithEnv([]string{"search", "--help"}, nil)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "kura search") || !strings.Contains(out, "--api-url") {
		t.Fatalf("help missing: %q", out)
	}
	// --api-url help should mention env
	if !strings.Contains(out, "KURA_API_URL") {
		t.Fatalf("help should mention env var, got %q", out)
	}
}

func TestRootHelp(t *testing.T) {
	code, out, _ := runWithEnv([]string{"--help"}, nil)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "kura <command>") {
		t.Fatalf("root help missing: %q", out)
	}
}

func TestPostGet_NotInvented(t *testing.T) {
	code, _, errOut := runWithEnv([]string{"post", "get", "123"}, nil)
	if code == 0 {
		t.Fatal("expected non-zero for post get (no endpoint)")
	}
	if !strings.Contains(errOut, "not available") {
		t.Fatalf("expected not available, got %q", errOut)
	}
}

func TestSearch_MissingFlagValue(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, _, errOut := runWithEnv([]string{"search", "--api-url"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected non-zero for missing value")
	}
	if !strings.Contains(errOut, "requires a value") {
		t.Fatalf("expected requires a value, got %q", errOut)
	}
}

func TestSearch_MultiWordQuery(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[]}`)
	}))
	defer srv.Close()

	code, _, _ := runWithEnv([]string{"search", "cat", "feline"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if gotQ != "cat feline" {
		t.Fatalf("multiword mismatch: got %q", gotQ)
	}
}
