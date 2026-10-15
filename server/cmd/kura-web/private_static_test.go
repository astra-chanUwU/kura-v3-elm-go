package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// This test captures the private-library boundary: files in the frontend's
// media directory must not remain readable through the static file server.
func TestStaticServerDoesNotExposeMediaTree(t *testing.T) {
	root := t.TempDir()
	mediaDir := filepath.Join(root, "static", "media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "private-fixture.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebHandler(root, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/static/media/private-fixture.txt", nil))
	if rec.Code == 200 && rec.Body.String() == "private" {
		t.Fatal("private media was readable through the public static file server")
	}
}
