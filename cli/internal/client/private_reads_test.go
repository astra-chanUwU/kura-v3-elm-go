package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func librarySuccessJSON(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/collections/") && strings.HasSuffix(r.URL.Path, "/export"):
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not here"}`))
	case r.URL.Path == "/api/posts/export":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not here"}`))
	case r.URL.Path == "/api/posts/trash":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[],"next_cursor":null}`))
	case r.URL.Path == "/api/collections":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collections":[]}`))
	case strings.HasPrefix(r.URL.Path, "/api/collections/"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	case strings.HasPrefix(r.URL.Path, "/api/posts/"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"7","preview_url":"/p","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"tags":[]}`))
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}
}

func TestPrivateReadsDefaultSendsNoBearerOnLibraryGETs(t *testing.T) {
	authByPath := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authByPath[r.URL.Path] = r.Header.Get("Authorization")
		librarySuccessJSON(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "secret"
	if c.PrivateReads {
		t.Fatal("New must default PrivateReads to false")
	}
	ctx := context.Background()
	if _, err := c.Search(ctx, "cat"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListCollections(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectionPosts(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetPostDetail(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	for path, auth := range authByPath {
		if auth != "" {
			t.Fatalf("default GET %s authorization=%q, want empty", path, auth)
		}
	}
	for _, want := range []string{"/api/posts", "/api/collections", "/api/collections/7/posts", "/api/posts/7"} {
		if _, ok := authByPath[want]; !ok {
			t.Fatalf("missing request to %s: %v", want, authByPath)
		}
	}
}

func TestPrivateReadsDefaultZIPExportSendsNoBearer(t *testing.T) {
	archive := makeExportZIP(t)
	var auth string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "secret"
	out := filepath.Join(t.TempDir(), "out.zip")
	if _, err := c.ExportPosts(context.Background(), []string{"1"}, false, out); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || auth != "" {
		t.Fatalf("default ZIP authorization=%q calls=%d, want empty/1", auth, calls)
	}
	out2 := filepath.Join(t.TempDir(), "c.zip")
	c2 := New(srv.URL)
	c2.APIToken = "secret"
	if _, err := c2.ExportCollection(context.Background(), "7", 0, false, out2); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("default collection ZIP authorization=%q, want empty", auth)
	}
}

func TestPrivateReadsOptInSendsBearerAcrossLibraryReads(t *testing.T) {
	authByPath := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authByPath[r.URL.Path] = r.Header.Get("Authorization")
		librarySuccessJSON(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "secret"
	c.PrivateReads = true
	ctx := context.Background()
	if _, err := c.Search(ctx, "cat"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListCollections(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectionPosts(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetPostDetail(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/api/posts", "/api/collections", "/api/collections/7/posts", "/api/posts/7"} {
		if got := authByPath[want]; got != "Bearer secret" {
			t.Fatalf("opt-in GET %s authorization=%q, want bearer", want, got)
		}
	}
}

func TestPrivateReadsOptInZIPExportsSendBearer(t *testing.T) {
	archive := makeExportZIP(t)
	authByPath := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authByPath[r.URL.Path] = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "secret"
	c.PrivateReads = true
	dir := t.TempDir()
	if _, err := c.ExportPosts(context.Background(), []string{"1"}, false, filepath.Join(dir, "a.zip")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExportCollection(context.Background(), "7", 0, false, filepath.Join(dir, "b.zip")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/api/posts/export", "/api/collections/7/export"} {
		if got := authByPath[want]; got != "Bearer secret" {
			t.Fatalf("opt-in ZIP %s authorization=%q, want bearer", want, got)
		}
	}
}

func TestPrivateReadsProtectedTrashStillWorksByDefault(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[],"next_cursor":null}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "tok"
	if _, err := c.ListTrash(context.Background(), "", 0); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tok" {
		t.Fatalf("protected trash authorization=%q, want bearer", auth)
	}
}

func TestPrivateReadsOffOriginRedirectNeverLeaksCredentials(t *testing.T) {
	evilHits := 0
	var evilAuth []string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		evilAuth = append(evilAuth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer evil.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/copy", http.StatusFound)
	}))
	defer api.Close()

	c := New(api.URL)
	c.APIToken = "secret"
	c.PrivateReads = true
	if _, err := c.Search(context.Background(), "cat"); err == nil {
		t.Fatal("expected redirect to fail closed")
	}
	if evilHits != 0 {
		t.Fatalf("off-origin redirect followed: hits=%d auth=%v", evilHits, evilAuth)
	}
	for _, got := range evilAuth {
		if got != "" {
			t.Fatalf("credential leaked to off-origin: %q", got)
		}
	}
}

func TestPrivateReadsZIPOffOriginRedirectNeverLeaksCredentials(t *testing.T) {
	evilHits := 0
	var evilAuth []string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		evilAuth = append(evilAuth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(makeExportZIP(t))
	}))
	defer evil.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/copy.zip", http.StatusFound)
	}))
	defer api.Close()

	c := New(api.URL)
	c.APIToken = "secret"
	c.PrivateReads = true
	out := filepath.Join(t.TempDir(), "out.zip")
	if _, err := c.ExportPosts(context.Background(), []string{"1"}, false, out); err == nil {
		t.Fatal("expected ZIP redirect to fail closed")
	}
	if evilHits != 0 {
		t.Fatalf("off-origin ZIP redirect followed: hits=%d auth=%v", evilHits, evilAuth)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("redirect must not publish a file: stat=%v", err)
	}
}

func TestPrivateReadsNewDefaultsFalseWithoutAmbient(t *testing.T) {
	t.Setenv("KURA_API_TOKEN", "ambient-should-stay-in-token-only")
	t.Setenv("KURA_PRIVATE_READS", "true")
	c := New("http://localhost:8080")
	if c.PrivateReads {
		t.Fatal("New must not read ambient KURA_PRIVATE_READS; command layer opts in explicitly")
	}
}
