package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func parityZIP(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("originals/1.txt")
	_, _ = w.Write([]byte("hello"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPostMetadataCLI(t *testing.T) {
	calls := 0
	var raw, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		raw = string(data)
		var req struct {
			Posts []struct {
				Version int `json:"version"`
			} `json:"posts"`
			Source *string `json:"source"`
		}
		_ = json.Unmarshal(data, &req)
		source := ""
		if req.Source != nil {
			source = *req.Source
		}
		fmt.Fprintf(w, `{"posts":[{"id":"7","version":%d,"source":%q,"artist":"","changed":true}]}`, req.Posts[0].Version+1, source)
	}))
	defer srv.Close()
	env := map[string]string{"KURA_API_URL": srv.URL, "KURA_API_TOKEN": "tok"}
	code, out, errOut := runWithEnv([]string{"post", "metadata", "--version", "0", "--source", "  new  ", "7"}, env)
	if code != 0 || !strings.Contains(out, "7\tversion:1") || auth != "Bearer tok" {
		t.Fatalf("code=%d out=%q err=%q auth=%q", code, out, errOut, auth)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil || string(body["source"]) != `"new"` {
		t.Fatalf("body %s err=%v", raw, err)
	}
	if _, ok := body["artist"]; ok {
		t.Fatalf("omitted artist sent %s", raw)
	}
	// Explicit empty clears.
	code, _, _ = runWithEnv([]string{"post", "metadata", "--version=1", "--source=", "--jsonl", "7"}, env)
	if code != 0 || !strings.Contains(raw, `"source":""`) {
		t.Fatalf("clear code=%d raw=%s", code, raw)
	}
	callsBefore := calls
	for _, args := range [][]string{
		{"post", "metadata", "--source", "x", "7"},
		{"post", "metadata", "--version", "0", "7"},
		{"post", "metadata", "--version", "0", "--source", strings.Repeat("é", 513), "7"},
		{"post", "metadata", "--version", "0", "--source", "x", "007"},
		{"post", "metadata", "--version", "0", "--source", "x", "7", "8"},
		{"post", "metadata", "--version", "0", "--source", "x", "--json=true", "7"},
		{"post", "metadata", "--version", "0", "--unknown", "x", "7"},
		{"post", "metadata", "--version", "0", "--source"},
		{"post", "metadata", "--version", "0", "--source", "x", "--", "--json"},
	} {
		if code, _, _ := runWithEnv(args, env); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	if calls != callsBefore {
		t.Fatalf("invalid made requests calls=%d before=%d", calls, callsBefore)
	}
	code, out, _ = runWithEnv([]string{"post", "metadata", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "--version") {
		t.Fatalf("help %d %q", code, out)
	}
}

func TestPostMetadataHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"error":"post metadata version is stale"}`)
	}))
	defer srv.Close()
	code, out, errOut := runWithEnv([]string{"post", "metadata", "--version", "0", "--artist", "a", "7"}, map[string]string{"KURA_API_URL": srv.URL})
	if code == 0 || out != "" || !strings.Contains(errOut, "409") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPostTrashRestoreCLI(t *testing.T) {
	calls := 0
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		data, _ := io.ReadAll(r.Body)
		raw = string(data)
		if r.URL.Path == "/api/posts/trash" {
			fmt.Fprint(w, `{"posts":[{"id":"2","changed":true},{"id":"1","changed":true}]}`)
			return
		}
		fmt.Fprint(w, `{"posts":[{"id":"1","changed":false}]}`)
	}))
	defer srv.Close()
	env := map[string]string{"KURA_API_URL": srv.URL, "KURA_API_TOKEN": "tok"}
	code, out, errOut := runWithEnv([]string{"post", "trash", "--reason", "  dup ", "1", "2"}, env)
	if code != 0 || !strings.Contains(raw, `"post_ids":["1","2"]`) || !strings.Contains(raw, `"reason":"dup"`) || !strings.HasPrefix(out, "2\tchanged:true") {
		t.Fatalf("code=%d out=%q err=%q raw=%s", code, out, errOut, raw)
	}
	code, out, _ = runWithEnv([]string{"post", "restore", "--jsonl", "1"}, env)
	if code != 0 || !strings.Contains(out, `"id":"1"`) || strings.Contains(raw, "reason") {
		t.Fatalf("restore code=%d out=%q raw=%s", code, out, raw)
	}
	before := calls
	many := []string{"post", "trash"}
	for i := 1; i <= 101; i++ {
		many = append(many, strconv.Itoa(i))
	}
	for _, args := range [][]string{
		{"post", "trash"},
		{"post", "trash", "1", "1"},
		{"post", "trash", "01"},
		many,
		{"post", "restore", "--reason", "x", "1"},
		{"post", "trash", "--reason=" + strings.Repeat("x", 513), "1"},
	} {
		if code, _, _ := runWithEnv(args, env); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	if calls != before {
		t.Fatalf("invalid calls=%d before=%d", calls, before)
	}
}

func TestPostTrashListCLI(t *testing.T) {
	var auth, rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		rawQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"posts":[{"id":"7","preview_url":"/p","original_url":"/o","media_type":"image/png","width":2,"height":3,"tags":[],"deleted_at":"2026-01-01T00:00:00Z","moderation_state":"published"}],"next_cursor":"next"}`)
	}))
	defer srv.Close()
	env := map[string]string{"KURA_API_URL": srv.URL, "KURA_API_TOKEN": "tok"}
	code, out, errOut := runWithEnv([]string{"post", "trash-list", "--limit", "5", "--cursor", "tok"}, env)
	if code != 0 || auth != "Bearer tok" || !strings.Contains(out, "2026-01-01") || !strings.Contains(errOut, "next_cursor: next") {
		t.Fatalf("code=%d out=%q err=%q auth=%q q=%q", code, out, errOut, auth, rawQuery)
	}
	code, out, _ = runWithEnv([]string{"post", "trash-list", "--jsonl"}, env)
	if code != 0 || strings.Count(strings.TrimSpace(out), "\n") != 0 || !strings.Contains(out, `"deleted_at"`) {
		t.Fatalf("jsonl code=%d out=%q", code, out)
	}
	code, out, _ = runWithEnv([]string{"post", "trash-list", "--json"}, env)
	if code != 0 || !strings.Contains(out, `"next_cursor"`) {
		t.Fatalf("json code=%d out=%q", code, out)
	}
	for _, args := range [][]string{
		{"post", "trash-list", "--limit", "0"},
		{"post", "trash-list", "--limit", "61"},
		{"post", "trash-list", "--cursor="},
		{"post", "trash-list", "7"},
	} {
		if code, _, _ := runWithEnv(args, env); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestExportCLI(t *testing.T) {
	archive := parityZIP(t)
	calls := 0
	var rawQuery, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		rawQuery, path, auth = r.URL.RawQuery, r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	env := map[string]string{"KURA_API_URL": srv.URL, "KURA_API_TOKEN": "tok"}
	dir := t.TempDir()
	outPath := filepath.Join(dir, "a.zip")
	code, out, errOut := runWithEnv([]string{"export", "--ids", "1,2", "--output", outPath}, env)
	if code != 0 || !strings.Contains(out, outPath) || !strings.Contains(out, "bytes") || auth != "" {
		t.Fatalf("code=%d out=%q err=%q auth=%q", code, out, errOut, auth)
	}
	if path != "/api/posts/export" || !strings.Contains(rawQuery, "metadata=false") {
		t.Fatalf("path=%q q=%q", path, rawQuery)
	}
	data, _ := os.ReadFile(outPath)
	if !bytes.Equal(data, archive) {
		t.Fatal("archive mismatch")
	}
	// JSON receipt and collection export.
	outPath2 := filepath.Join(dir, "b.zip")
	code, out, errOut = runWithEnv([]string{"collection", "export", "--version", "0", "--metadata", "--json", "--output", outPath2, "7"}, env)
	if code != 0 {
		t.Fatalf("collection export code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, `"path"`) || !strings.Contains(out, `"bytes"`) || path != "/api/collections/7/export" || !strings.Contains(rawQuery, "expected_version=0") || !strings.Contains(rawQuery, "metadata=true") {
		t.Fatalf("collection out=%q path=%q q=%q", out, path, rawQuery)
	}
	before := calls
	existing := filepath.Join(dir, "a.zip")
	for _, args := range [][]string{
		{"export", "--ids", "1,1", "--output", filepath.Join(dir, "x.zip")},
		{"export", "--ids", "01", "--output", filepath.Join(dir, "x.zip")},
		{"export", "--output", filepath.Join(dir, "x.zip")},
		{"export", "--ids", "1"},
		{"export", "--ids", "1", "--output", existing},
		{"export", "--ids", "1", "--output", filepath.Join(dir, "x.zip"), "extra"},
		{"export", "--ids", "1", "--output", filepath.Join(dir, "x.zip"), "--metadata=true"},
		{"collection", "export", "--output", filepath.Join(dir, "x.zip"), "7"},
		{"collection", "export", "--version", "0", "--output", filepath.Join(dir, "x.zip")},
	} {
		if code, _, _ := runWithEnv(args, env); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	if calls != before {
		t.Fatalf("invalid export calls=%d before=%d", calls, before)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.zip")); !os.IsNotExist(err) {
		t.Fatalf("x.zip should not exist err=%v", err)
	}
	code, out, _ = runWithEnv([]string{"export", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "--ids") {
		t.Fatalf("export help %d %q", code, out)
	}
	code, out, _ = runWithEnv([]string{"collection", "export", "--help"}, nil)
	if code != 0 || !strings.Contains(out, "--version") {
		t.Fatalf("collection export help %d %q", code, out)
	}
}

func TestExportCLICancelCleansTemp(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.zip")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	var stdout, stderr bytes.Buffer
	code := runContext(ctx, []string{"export", "--ids", "1", "--output", outPath}, &stdout, &stderr, func(k string) string {
		if k == "KURA_API_URL" {
			return srv.URL
		}
		return ""
	})
	if code == 0 {
		t.Fatal("expected cancel failure")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("leftover entries %v", entries)
	}
}
