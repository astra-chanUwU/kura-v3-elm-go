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

func TestSearch_Pagination_Params(t *testing.T) {
	var gotQ, gotCursor, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		gotCursor = r.URL.Query().Get("cursor")
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[{"id":"post-123","preview_url":"/p.jpg","original_url":"/o.jpg","media_type":"image/jpeg","width":10,"height":10}],"next_cursor":null}`)
	}))
	defer srv.Close()

	code, out, _ := runWithEnv([]string{"search", "--limit", "20", "--cursor", "tok123", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if gotQ != "cat" || gotCursor != "tok123" || gotLimit != "20" {
		t.Fatalf("pagination not wired q=%q cursor=%q limit=%q", gotQ, gotCursor, gotLimit)
	}
	if !strings.Contains(out, "post-123") {
		t.Fatalf("output missing post: %q", out)
	}

	// --limit with = form and --cursor with = form
	gotQ, gotCursor, gotLimit = "", "", ""
	code, _, _ = runWithEnv([]string{"search", "--limit=5", "--cursor=abc==", "hello"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if gotLimit != "5" || gotCursor != "abc==" {
		t.Fatalf("equals form not parsed limit=%q cursor=%q", gotLimit, gotCursor)
	}
}

func TestSearch_Pagination_LimitValidation(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	invalid := []string{"0", "61", "-1", "abc", "100", ""}
	for _, v := range invalid {
		args := []string{"search", "--limit", v, "cat"}
		if v == "" {
			args = []string{"search", "--limit=", "cat"}
		}
		code, _, errOut := runWithEnv(args, map[string]string{"KURA_API_URL": srv.URL})
		if code == 0 {
			t.Fatalf("expected failure for limit %q", v)
		}
		if !strings.Contains(strings.ToLower(errOut), "limit") {
			t.Fatalf("expected limit error for %q got %q", v, errOut)
		}
	}
	// valid boundaries
	for _, v := range []string{"1", "60", "20"} {
		code, _, errOut := runWithEnv([]string{"search", "--limit", v, "cat"}, map[string]string{"KURA_API_URL": srv.URL})
		if code != 0 {
			t.Fatalf("limit %q should pass, got %d %q", v, code, errOut)
		}
	}
}

func TestSearch_Pagination_CursorValidation(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, _, errOut := runWithEnv([]string{"search", "--cursor", "", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected failure for empty cursor")
	}
	if !strings.Contains(strings.ToLower(errOut), "cursor") {
		t.Fatalf("expected cursor error got %q", errOut)
	}

	code, _, errOut = runWithEnv([]string{"search", "--cursor", "   ", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 {
		t.Fatal("expected failure for whitespace cursor")
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
