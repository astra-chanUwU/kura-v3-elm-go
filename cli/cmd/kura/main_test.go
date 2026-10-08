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
			fmt.Fprint(w, `{"posts":[{"id":"post-newest","preview_url":"/media/newest.jpg","original_url":"/media/newest-original.jpg","media_type":"image/jpeg","width":640,"height":480}],"next_cursor":"browse-next"}`)
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

func TestSearch_EmptyBrowse(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"search"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if !strings.Contains(out, "post-newest") {
		t.Fatalf("expected newest browse row, got %q", out)
	}
	if !strings.Contains(errOut, "browse-next") {
		t.Fatalf("expected browse cursor hint, got %q", errOut)
	}
	// empty --json should produce envelope
	code, out, _ = runWithEnv([]string{"search", "--json"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, `"posts"`) || !strings.Contains(out, "post-newest") || !strings.Contains(out, "browse-next") {
		t.Fatalf("expected json posts, got %q", out)
	}
	// --jsonl browse should produce one row and the cursor hint
	code, out, errOut = runWithEnv([]string{"search", "--jsonl"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "post-newest") {
		t.Fatalf("expected browse jsonl row, got %q", out)
	}
	if !strings.Contains(errOut, "browse-next") {
		t.Fatalf("expected browse cursor hint, got %q", errOut)
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

func collectionServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected collections method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"collections":[{"id":"7","name":"Favorites","post_ids":["11","10"]},{"id":"8","name":"Archive","post_ids":[]}]}`)
	})
	mux.HandleFunc("/api/collections/7/posts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected posts method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[{"id":"11","preview_url":"/p.jpg","original_url":"/o.jpg","media_type":"image/jpeg","width":10,"height":20}],"next_cursor":"next"}`)
	})
	mux.HandleFunc("/api/collections/404/posts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"collection or post not found"}`)
	})
	return httptest.NewServer(mux)
}

func TestCollectionList_HumanAndJSON(t *testing.T) {
	srv := collectionServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"collection", "list", "--api-url", srv.URL}, nil)
	if code != 0 || errOut != "" {
		t.Fatalf("list human exit=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, "7\tFavorites\t2 posts") || !strings.Contains(out, "8\tArchive\t0 posts") {
		t.Fatalf("unexpected human collection output: %q", out)
	}
	code, out, _ = runWithEnv([]string{"collection", "list", "--json"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, `"collections"`) || !strings.Contains(out, "Favorites") {
		t.Fatalf("unexpected json collection output code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"collection", "list", "--jsonl", "--api-url=" + srv.URL}, nil)
	if code != 0 || strings.Count(strings.TrimSpace(out), "\n") != 1 || !strings.Contains(out, `"id":"7"`) {
		t.Fatalf("unexpected jsonl collection output code=%d out=%q", code, out)
	}
}

func TestCollectionPosts_OutputAndErrors(t *testing.T) {
	srv := collectionServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"collection", "posts", "--api-url", srv.URL, "7"}, nil)
	if code != 0 || !strings.Contains(out, "11\timage/jpeg\t10x20") || !strings.Contains(errOut, "next") {
		t.Fatalf("unexpected posts human exit=%d out=%q err=%q", code, out, errOut)
	}
	code, out, _ = runWithEnv([]string{"collection", "posts", "--jsonl", "7"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, `"id":"11"`) {
		t.Fatalf("unexpected posts jsonl exit=%d out=%q", code, out)
	}
	code, out, errOut = runWithEnv([]string{"collection", "posts", "--json", "404"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 || out != "" || !strings.Contains(errOut, "collection or post not found") {
		t.Fatalf("expected readable API error code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestCollection_HelpAndValidation(t *testing.T) {
	code, out, _ := runWithEnv([]string{"collection", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "kura collection") || !strings.Contains(out, "posts") {
		t.Fatalf("collection help missing: code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"collection", "posts", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "COLLECTION_ID") {
		t.Fatalf("collection posts help missing: code=%d out=%q", code, out)
	}
	code, _, errOut := runWithEnv([]string{"collection", "posts", "not-an-id"}, nil)
	if code == 0 || !strings.Contains(errOut, "positive integer") {
		t.Fatalf("expected id validation error: code=%d err=%q", code, errOut)
	}
	code, _, errOut = runWithEnv([]string{"collection", "list", "--json", "--jsonl"}, nil)
	if code == 0 || !strings.Contains(errOut, "mutually exclusive") {
		t.Fatalf("expected output flag validation error: code=%d err=%q", code, errOut)
	}
}

func collectionMutationServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected create method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"9","name":"Reference","post_ids":[]}`)
	})
	mux.HandleFunc("/api/collections/9/posts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected add method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"9","name":"Reference","post_ids":["11","10"]}`)
	})
	mux.HandleFunc("/api/collections/9/posts/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/collections/9/posts/11" {
			t.Fatalf("unexpected remove request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/collections/9/order", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected reorder method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"9","name":"Reference","post_ids":["10","11"]}`)
	})
	return httptest.NewServer(mux)
}

func TestCollectionMutations_Output(t *testing.T) {
	srv := collectionMutationServer(t)
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"collection", "create", "--api-url", srv.URL, "Reference"}, nil)
	if code != 0 || errOut != "" || !strings.Contains(out, "9\tReference\t0 posts") {
		t.Fatalf("create output code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = runWithEnv([]string{"collection", "add", "--json", "--api-url", srv.URL, "9", "11", "10"}, nil)
	if code != 0 || errOut != "" || !strings.Contains(out, `"post_ids"`) {
		t.Fatalf("add output code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = runWithEnv([]string{"collection", "remove", "--jsonl", "--api-url=" + srv.URL, "9", "11"}, nil)
	if code != 0 || errOut != "" || !strings.Contains(out, `"removed":true`) {
		t.Fatalf("remove output code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = runWithEnv([]string{"collection", "reorder", "--api-url", srv.URL, "9", "10", "11"}, nil)
	if code != 0 || errOut != "" || !strings.Contains(out, "9\tReference\t2 posts") {
		t.Fatalf("reorder output code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestCollectionMutations_ValidationAndHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"collection", "create"}, "exactly one NAME"},
		{[]string{"collection", "add", "9"}, "at least one POST_ID"},
		{[]string{"collection", "remove", "9"}, "POST_ID"},
		{[]string{"collection", "reorder", "bad"}, "collection id must be a positive integer"},
	} {
		code, _, errOut := runWithEnv(tc.args, nil)
		if code == 0 || !strings.Contains(errOut, tc.want) {
			t.Fatalf("args=%v code=%d expected %q in %q", tc.args, code, tc.want, errOut)
		}
	}
	code, out, _ := runWithEnv([]string{"collection", "create", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "NAME") {
		t.Fatalf("create help missing: code=%d out=%q", code, out)
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

func postDetailServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/posts/2004", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"2004","preview_url":"/preview","original_url":"/original","media_type":"image/jpeg","width":679,"height":437,"source":"demo/kson.jpeg","artist":"Kura Demo","hash":"sha256:test","file_size":165554,"created_at":"2026-01-02 00:00:04+00","tags":["demo","kson"],"tag_version":2,"favorite":true,"score":3,"reaction_version":1,"history":[{"version":2,"kind":"tag_edit","added_tags":["night"],"removed_tags":["demo"],"target_tags":["kson","night"],"created_at":"2026-01-02 00:00:06+00"},{"version":1,"kind":"tag_edit","added_tags":["demo"],"removed_tags":[],"target_tags":["demo","kson"],"created_at":"2026-01-02 00:00:05+00"}],"reaction_history":[{"version":1,"favorite":true,"score":3,"created_at":"2026-01-03 00:00:00+00"}]}`)
	})
	mux.HandleFunc("/api/posts/404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"post not found"}`)
	})
	mux.HandleFunc("/api/posts/tags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected tags method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[{"id":"2004","version":3,"tags":["kson","night"],"changed":true}]}`)
	})
	mux.HandleFunc("/api/posts/tags/revert", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected revert method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[{"id":"2004","version":4,"tags":["demo","kson"],"changed":true}]}`)
	})
	mux.HandleFunc("/api/posts/reactions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected reactions method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"posts":[{"id":"2004","version":2,"favorite":true,"score":5,"changed":true}]}`)
	})
	return httptest.NewServer(mux)
}

func TestPostShow_HumanJSONJSONL(t *testing.T) {
	srv := postDetailServer(t)
	defer srv.Close()

	code, out, _ := runWithEnv([]string{"post", "show", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("show human exit %d", code)
	}
	if !strings.Contains(out, "ID: 2004") || !strings.Contains(out, "Source:") || !strings.Contains(out, "Tag Version: 2") || !strings.Contains(out, "History:") || !strings.Contains(out, "Reaction History:") {
		t.Fatalf("human detail missing fields: %q", out)
	}
	if !strings.Contains(out, "night") || !strings.Contains(out, "tag_edit") {
		t.Fatalf("human history missing: %q", out)
	}
	code, out, _ = runWithEnv([]string{"post", "show", "--json", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, `"id": "2004"`) || !strings.Contains(out, `"history"`) || !strings.Contains(out, `"reaction_history"`) {
		t.Fatalf("json detail missing code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "show", "--jsonl", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 {
		t.Fatalf("jsonl exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"id":"2004"`) || !strings.Contains(lines[0], `"history"`) {
		t.Fatalf("jsonl detail unexpected: %q", out)
	}
}

func TestPostShow_ErrorsAndValidation(t *testing.T) {
	srv := postDetailServer(t)
	defer srv.Close()

	code, _, errOut := runWithEnv([]string{"post", "show", "404"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 || !strings.Contains(errOut, "post not found") {
		t.Fatalf("expected not found code=%d err=%q", code, errOut)
	}
	code, _, errOut = runWithEnv([]string{"post", "show", "not-an-id"}, nil)
	if code == 0 || !strings.Contains(errOut, "positive integer") {
		t.Fatalf("expected id validation err=%q", errOut)
	}
	code, _, errOut = runWithEnv([]string{"post", "show", "--json", "--jsonl", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 || !strings.Contains(errOut, "mutually exclusive") {
		t.Fatalf("expected json flag validation err=%q", errOut)
	}
	code, out, _ := runWithEnv([]string{"post", "show", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "kura post show") {
		t.Fatalf("show help missing: %q", out)
	}
}

func TestPostTagEdit_FavoriteScore_Reaction(t *testing.T) {
	srv := postDetailServer(t)
	defer srv.Close()

	code, out, _ := runWithEnv([]string{"post", "tag", "edit", "--version", "2", "--add", "night", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "2004") || !strings.Contains(out, "version:3") {
		t.Fatalf("tag edit human failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "tag", "edit", "--version=2", "--json", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, `"version"`) {
		t.Fatalf("tag edit json failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "tag", "edit", "--version", "2", "--jsonl", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, `"id":"2004"`) {
		t.Fatalf("tag edit jsonl failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "tag", "revert", "--version", "3", "--target-version", "1", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "version:4") {
		t.Fatalf("tag revert failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "favorite", "--version", "1", "--favorite", "true", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "favorite:true") {
		t.Fatalf("favorite failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "score", "--version", "1", "--score", "5", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "score:5") {
		t.Fatalf("score failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "reaction", "--version", "1", "--favorite", "true", "--score", "5", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "favorite:true") || !strings.Contains(out, "score:5") {
		t.Fatalf("reaction failed code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "reaction", "--json", "--version", "1", "--favorite=true", "2004"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, `"favorite"`) {
		t.Fatalf("reaction json failed code=%d out=%q", code, out)
	}
}

func TestPostHelpAndValidation(t *testing.T) {
	code, out, _ := runWithEnv([]string{"post", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "kura post") || !strings.Contains(out, "show") {
		t.Fatalf("post help missing: %q", out)
	}
	code, out, _ = runWithEnv([]string{"post", "tag", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "edit") || !strings.Contains(out, "revert") {
		t.Fatalf("post tag help missing: %q", out)
	}
	code, _, errOut := runWithEnv([]string{"post", "tag", "edit", "2004"}, nil)
	if code == 0 || !strings.Contains(errOut, "--version") {
		t.Fatalf("expected version required err=%q", errOut)
	}
	code, _, errOut = runWithEnv([]string{"post", "favorite", "--version", "1", "2004"}, nil)
	if code == 0 || !strings.Contains(errOut, "--favorite") {
		t.Fatalf("expected favorite required err=%q", errOut)
	}
	code, _, errOut = runWithEnv([]string{"post", "score", "--version", "1", "2004"}, nil)
	if code == 0 || !strings.Contains(errOut, "--score") {
		t.Fatalf("expected score required err=%q", errOut)
	}
	code, _, errOut = runWithEnv([]string{"post", "unknown"}, nil)
	if code == 0 || !strings.Contains(errOut, "unknown post command") {
		t.Fatalf("expected unknown command err=%q", errOut)
	}
}
