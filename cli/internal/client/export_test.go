package client

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
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

func makeExportZIP(t *testing.T) []byte {
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

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestExportPostsSuccessAndCollectionQuery(t *testing.T) {
	archive := makeExportZIP(t)
	var rawQuery, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery, path, auth = r.URL.RawQuery, r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.zip")
	c := New(srv.URL)
	c.APIToken = "tok"
	result, err := c.ExportPosts(context.Background(), []string{"1", "2"}, true, out)
	if err != nil || result.Bytes != int64(len(archive)) || result.Path != out {
		t.Fatalf("result %#v err=%v", result, err)
	}
	if path != "/api/posts/export" || !strings.Contains(rawQuery, "ids=1%2C2") || !strings.Contains(rawQuery, "metadata=true") || auth != "" {
		t.Fatalf("path=%q query=%q auth=%q", path, rawQuery, auth)
	}
	data, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(data, archive) {
		t.Fatalf("file err=%v equal=%v", err, bytes.Equal(data, archive))
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", info.Mode().Perm())
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Fatalf("leftover %v", names)
	}
	out2 := filepath.Join(dir, "c.zip")
	if _, err := c.ExportCollection(context.Background(), "7", 0, false, out2); err != nil {
		t.Fatal(err)
	}
	if path != "/api/collections/7/export" || !strings.Contains(rawQuery, "expected_version=0") || !strings.Contains(rawQuery, "metadata=false") {
		t.Fatalf("collection path=%q query=%q", path, rawQuery)
	}
}

func TestExportRefusesExistingBeforeHTTP(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.zip")
	if err := os.WriteFile(out, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(srv.URL).ExportPosts(context.Background(), []string{"1"}, false, out); err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "keep" {
		t.Fatalf("overwrote %q", data)
	}
}

func TestExportRejectsBadArchivesAndCleansTemp(t *testing.T) {
	archive := makeExportZIP(t)
	for _, tc := range []struct {
		name string
		fn   func(w http.ResponseWriter, r *http.Request)
	}{
		{"wrong type", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(archive)
		}},
		{"non zip", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			fmt.Fprint(w, "not a zip")
		}},
		{"truncated", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			cut := archive[:len(archive)/2]
			w.Header().Set("Content-Length", strconv.Itoa(len(cut)))
			_, _ = w.Write(cut)
		}},
		{"oversized known", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Length", strconv.Itoa(maxExportZipBytes+1))
			w.WriteHeader(200)
		}},
		{"http error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"error":"collection changed","code":"collection_changed"}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(tc.fn))
			defer srv.Close()
			dir := t.TempDir()
			out := filepath.Join(dir, "out.zip")
			_, err := New(srv.URL).ExportPosts(context.Background(), []string{"1"}, false, out)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.name == "http error" {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != "collection_changed" {
					t.Fatalf("api err %v", err)
				}
			}
			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("destination should not exist, stat=%v", statErr)
			}
			if names := dirNames(t, dir); len(names) != 0 {
				t.Fatalf("leftover temp %v", names)
			}
		})
	}
}

func TestExportRedirectNotFollowed(t *testing.T) {
	archive := makeExportZIP(t)
	destCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/posts/export", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/destination", 302) })
	mux.HandleFunc("/destination", func(w http.ResponseWriter, r *http.Request) {
		destCalls++
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.zip")
	if _, err := New(srv.URL).ExportPosts(context.Background(), []string{"1"}, false, out); err == nil || destCalls != 0 {
		t.Fatalf("err=%v destCalls=%d", err, destCalls)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("leftover %v", names)
	}
}

func TestExportConcurrentPublicationAndCancelCleanup(t *testing.T) {
	archive := makeExportZIP(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.zip")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = os.WriteFile(out, []byte("racer"), 0o644)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	_, err := New(srv.URL).ExportPosts(context.Background(), []string{"1"}, false, out)
	srv.Close()
	if err == nil {
		t.Fatal("expected link race error")
	}
	data, _ := os.ReadFile(out)
	if string(data) != "racer" {
		t.Fatalf("racer overwritten %q", data)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Fatalf("leftover %v", names)
	}

	started := make(chan struct{})
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv2.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	out2 := filepath.Join(dir, "cancel.zip")
	if _, err := New(srv2.URL).ExportPosts(ctx, []string{"1"}, false, out2); err == nil {
		t.Fatal("expected cancel error")
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Fatalf("cancel leftover %v", names)
	}
}

// Cancellation at EOF must not publish a successfully received archive.
type cancelExportBody struct {
	*bytes.Reader
	cancel context.CancelFunc
}

func (b cancelExportBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		b.cancel()
	}
	return n, err
}
func (b cancelExportBody) Close() error { return nil }

type exportRoundTrip func(*http.Request) (*http.Response, error)

func (f exportRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExportCancelledAtEOFDoesNotPublish(t *testing.T) {
	archive := makeExportZIP(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New("http://localhost")
	c.HTTPClient = &http.Client{Transport: exportRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/zip"}}, ContentLength: -1,
			Body: cancelExportBody{bytes.NewReader(archive), cancel}}, nil
	})}
	dir := t.TempDir()
	_, err := c.ExportPosts(ctx, []string{"1"}, false, filepath.Join(dir, "cancelled.zip"))
	if !errors.Is(err, context.Canceled) || len(dirNames(t, dir)) != 0 {
		t.Fatalf("err=%v leftover=%v", err, dirNames(t, dir))
	}
}
