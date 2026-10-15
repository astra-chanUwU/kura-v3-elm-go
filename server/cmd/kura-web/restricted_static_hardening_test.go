package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRestrictedStaticAllowlistAndMediaDenialHardening(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "index.html", "Kura UI")
	writeFixture(t, root, "kura.css", "css")
	writeFixture(t, root, "browser-import.js", "js")
	writeFixture(t, root, "dist/elm.js", "elm")
	writeFixture(t, root, "dist/elm-tests.js", "tests-js")
	writeFixture(t, root, "static/icons/shelf.svg", "<svg/>")
	writeFixture(t, root, "static/media/private-fixture.txt", "private")
	writeFixture(t, root, "static/media/demo/secret.jpg", "private-image")
	writeFixture(t, root, "src/Main.elm", "source")
	writeFixture(t, root, "elm.json", "{}")
	writeFixture(t, root, ".hidden", "hidden")

	handler, err := newWebHandler(root, "")
	if err != nil {
		t.Fatal(err)
	}
	serve := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	allowed := map[string]string{
		"/":                                "Kura UI",
		"/index.html":                      "Kura UI",
		"/kura.css":                        "css",
		"/browser-import.js":               "js",
		"/dist/elm.js":                     "elm",
		"/static/icons/shelf.svg":          "<svg/>",
		"/static/icons/../icons/shelf.svg": "<svg/>",
	}
	for target, want := range allowed {
		if rec := serve(target); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Fatalf("allowed %s = %d %q, want 200 %q", target, rec.Code, rec.Body.String(), want)
		}
	}

	denied := []string{
		"/static/media/private-fixture.txt",
		"/static/media/demo/secret.jpg",
		"/static/media",
		"/static/media/",
		"/static/%6dedia/private-fixture.txt",
		"/static/media%2fprivate-fixture.txt",
		"/static/./media/private-fixture.txt",
		"/static/icons/../media/private-fixture.txt",
		"/browser-import.js/../static/media/private-fixture.txt",
		"/%73tatic/media/private-fixture.txt",
		"/src/Main.elm",
		"/tests/TestRunner.elm",
		"/dist/elm-tests.js",
		"/elm.json",
		"/.hidden",
		"/static/icons/",
		"/static/",
	}
	for _, target := range denied {
		if rec := serve(target); rec.Code == http.StatusOK {
			t.Fatalf("denied %s served 200 %q", target, rec.Body.String())
		} else if rec.Code != http.StatusNotFound {
			t.Fatalf("denied %s = %d, want 404", target, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/kura.css", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST static = %d, want 405", rec.Code)
	}
}
