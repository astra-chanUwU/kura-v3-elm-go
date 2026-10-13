package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

func TestPagedReadsAndAllOutput(t *testing.T) {
	for _, command := range [][]string{{"search"}, {"collection", "posts"}} {
		for _, format := range []string{"", "--jsonl"} {
			t.Run(strings.Join(command, "-")+format, func(t *testing.T) {
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.URL.Query().Get("limit") != "1" {
						t.Errorf("limit=%q", r.URL.RawQuery)
					}
					if command[0] == "collection" && r.URL.Path != "/api/collections/7/posts" {
						t.Errorf("path=%s", r.URL.Path)
					}
					if requests == 1 {
						fmt.Fprint(w, `{"posts":[{"id":"1"}],"next_cursor":"a+&/=","collection_version":5}`)
					} else {
						if r.URL.Query().Get("cursor") != "a+&/=" {
							t.Errorf("cursor=%q", r.URL.RawQuery)
						}
						fmt.Fprint(w, `{"posts":[{"id":"2"}],"next_cursor":null,"collection_version":5}`)
					}
				}))
				defer srv.Close()
				args := append(append([]string{}, command...), "--all", "--limit", "1")
				if format != "" {
					args = append(args, format)
				}
				if command[0] == "collection" {
					args = append(args, "7")
				}
				code, out, errOut := runWithEnv(args, map[string]string{"KURA_API_URL": srv.URL})
				if code != 0 || errOut != "" || requests != 2 {
					t.Fatalf("code=%d requests=%d stderr=%s", code, requests, errOut)
				}
				if len(strings.Split(strings.TrimSpace(out), "\n")) != 2 {
					t.Fatalf("out=%q", out)
				}
			})
		}
	}
}

func TestCollectionExplicitPageAndValidation(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("cursor") != "opaque+=" || r.URL.Query().Get("limit") != "60" {
			t.Errorf("query=%q", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"posts":[],"next_cursor":"next"}`)
	}))
	defer srv.Close()
	code, _, errOut := runWithEnv([]string{"collection", "posts", "--cursor=opaque+=", "--limit=60", "7"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || requests != 1 || !strings.Contains(errOut, "next_cursor: next") {
		t.Fatalf("code=%d requests=%d stderr=%s", code, requests, errOut)
	}
	for _, args := range [][]string{
		{"collection", "posts", "--limit", "61", "7"},
		{"collection", "posts", "--cursor=", "7"},
		{"collection", "list", "--all"},
		{"collection", "create", "--all", "test"},
	} {
		code, _, _ := runWithEnv(args, map[string]string{"KURA_API_URL": srv.URL})
		if code == 0 || requests != 1 {
			t.Fatalf("invalid args=%v code=%d requests=%d", args, code, requests)
		}
	}
}

func TestAllStopsOnLoopDuplicatesVersionChangesAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, second, message string
		status                int
	}{
		{"loop", `{"posts":[],"next_cursor":"next","collection_version":1}`, "repeated pagination cursor", 200},
		{"empty-cursor", `{"posts":[],"next_cursor":"","collection_version":1}`, "empty or repeated", 200},
		{"duplicate", `{"posts":[{"id":"1"}],"collection_version":1}`, "duplicate post ID", 200},
		{"version", `{"posts":[{"id":"2"}],"collection_version":2}`, "version changed", 200},
		{"missing-version", `{"posts":[{"id":"2"}]}`, "version changed", 200},
		{"conflict", `{"error":"collection changed","code":"collection_changed"}`, "api error 409", 409},
		{"unavailable", `{"error":"temporarily unavailable"}`, "api error 503", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					fmt.Fprint(w, `{"posts":[{"id":"1"}],"next_cursor":"next","collection_version":1}`)
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.second)
			}))
			defer srv.Close()
			code, _, errOut := runWithEnv([]string{"collection", "posts", "--all", "--jsonl", "7"}, map[string]string{"KURA_API_URL": srv.URL})
			if code == 0 || requests != 2 || !strings.Contains(errOut, tc.message) {
				t.Fatalf("code=%d requests=%d stderr=%s", code, requests, errOut)
			}
		})
	}
}

func TestAllHasPageBudget(t *testing.T) {
	requests := 0
	fetch := func(context.Context, string) (client.SearchResponse, error) {
		requests++
		cursor := fmt.Sprint(requests)
		return client.SearchResponse{Posts: []client.PostSummary{}, NextCursor: &cursor}, nil
	}
	var out, stderr bytes.Buffer
	code := readPostPages(context.Background(), searchConfig{allPages: true, jsonlOut: true}, "", fetch, &out, &stderr)
	if code == 0 || requests != 1000 || out.Len() != 0 || !strings.Contains(stderr.String(), `--cursor "1000"`) {
		t.Fatalf("code=%d requests=%d stderr=%s", code, requests, &stderr)
	}
}

type failedWriter struct{ short bool }

func (w failedWriter) Write(p []byte) (int, error) {
	if w.short {
		return 0, nil
	}
	return 0, errors.New("disk full")
}

func TestOutputFailuresAreNonzeroAndStopPaging(t *testing.T) {
	for _, format := range []string{"", "--jsonl", "--json"} {
		for _, short := range []bool{false, true} {
			t.Run(fmt.Sprint(format, short), func(t *testing.T) {
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if requests == 1 {
						fmt.Fprint(w, `{"posts":[{"id":"1"}],"next_cursor":"next"}`)
					} else {
						fmt.Fprint(w, `{"posts":[{"id":"2"}]}`)
					}
				}))
				defer srv.Close()
				args := []string{"search", "--all"}
				if format != "" {
					args = append(args, format)
				}
				var stderr bytes.Buffer
				code := run(args, failedWriter{short: short}, &stderr, func(key string) string {
					if key == "KURA_API_URL" {
						return srv.URL
					}
					return ""
				})
				want := 1
				if format == "--json" {
					// --all --json is rejected before any request is made.
					want = 0
				}
				if code == 0 || requests != want || !strings.Contains(stderr.String(), "error:") {
					t.Fatalf("code=%d requests=%d stderr=%s", code, requests, &stderr)
				}
			})
		}
	}
	var stderr bytes.Buffer
	if code := run([]string{"--help"}, failedWriter{}, &stderr, nil); code == 0 {
		t.Fatal("help silently succeeded")
	}
}

func TestCLIEnvironmentCredentialsAndTimeout(t *testing.T) {
	t.Setenv("KURA_API_TOKEN", "ambient-token-must-not-be-used")
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer supplied-token" {
			t.Errorf("authorization=%q", got)
		}
		fmt.Fprint(w, `{"posts":[]}`)
	}))
	defer srv.Close()
	code, _, errOut := runWithEnv([]string{"post", "favorite", "--version", "0", "--favorite=true", "7"}, map[string]string{"KURA_API_URL": srv.URL, "KURA_API_TOKEN": "supplied-token"})
	if code != 0 || errOut != "" || requests != 1 {
		t.Fatalf("code=%d requests=%d stderr=%s", code, requests, errOut)
	}
	for _, value := range []string{"bad", "0s", "-1s"} {
		code, _, errOut := runWithEnv([]string{"search"}, map[string]string{"KURA_API_URL": srv.URL, "KURA_HTTP_TIMEOUT": value})
		if code == 0 || !strings.Contains(errOut, "KURA_HTTP_TIMEOUT") || requests != 1 {
			t.Fatalf("invalid timeout code=%d stderr=%s", code, errOut)
		}
	}
	c, err := newAPIClient(srv.URL, func(string) string { return "" })
	if err != nil || c.APIToken != "" {
		t.Fatalf("ambient environment leaked: %+v, %v", c, err)
	}
	c, err = newAPIClient(srv.URL, func(key string) string {
		if key == "KURA_HTTP_TIMEOUT" {
			return "25ms"
		}
		return ""
	})
	if err != nil || c.HTTPClient.Timeout != 25*time.Millisecond {
		t.Fatalf("timeout not applied: %+v %v", c, err)
	}
}

func TestCLIContextCancelsActiveRequest(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	var stderr bytes.Buffer
	code := runContext(ctx, []string{"search"}, io.Discard, &stderr, func(key string) string {
		if key == "KURA_API_URL" {
			return srv.URL
		}
		return ""
	})
	if code == 0 || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
}

func TestAllJSONIsRejectedBeforeFetching(t *testing.T) {
	for _, args := range [][]string{
		{"search", "--all", "--json", "cat"},
		{"collection", "posts", "--all", "--json", "7"},
	} {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			fmt.Fprint(w, `{"posts":[]}`)
		}))
		code, out, errOut := runWithEnv(args, map[string]string{"KURA_API_URL": srv.URL})
		srv.Close()
		if code == 0 || requests != 0 || out != "" || !strings.Contains(errOut, "buffer every page") || !strings.Contains(errOut, "--all --jsonl") {
			t.Fatalf("args=%v code=%d requests=%d out=%q stderr=%s", args, code, requests, out, errOut)
		}
	}
	// Single-page --json remains available.
	var stderr bytes.Buffer
	fetch := func(context.Context, string) (client.SearchResponse, error) {
		return client.SearchResponse{Posts: []client.PostSummary{{ID: "1"}}}, nil
	}
	var out bytes.Buffer
	if code := readPostPages(context.Background(), searchConfig{jsonOut: true}, "", fetch, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"id": "1"`) {
		t.Fatalf("single-page json code=%d out=%q stderr=%s", code, out.String(), stderr.String())
	}
}

func TestFlagParserPreservesLiteralAndRejectsBoolValues(t *testing.T) {
	var gotQ string
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotQ = r.URL.Query().Get("q")
		fmt.Fprint(w, `{"posts":[]}`)
	}))
	defer srv.Close()
	code, _, errOut := runWithEnv([]string{"search", "--", "--json"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || gotQ != "--json" || requests != 1 {
		t.Fatalf("literal after --: code=%d q=%q requests=%d stderr=%s", code, gotQ, requests, errOut)
	}
	code, _, errOut = runWithEnv([]string{"search", "--json=true", "cat"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 || requests != 1 || !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("bool flag with value: code=%d requests=%d stderr=%s", code, requests, errOut)
	}
	code, _, errOut = runWithEnv([]string{"post", "show", "--", "-5"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 || requests != 1 || !strings.Contains(errOut, "positive integer") {
		t.Fatalf("literal id after --: code=%d requests=%d stderr=%s", code, requests, errOut)
	}
}

func TestScoreOutsideServerRangeDoesNotSend(t *testing.T) {
	code, _, errOut := runWithEnv([]string{"post", "score", "--version=0", "--score=2147483648", "7"}, nil)
	if code == 0 || !strings.Contains(errOut, "2147483647") {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
}
