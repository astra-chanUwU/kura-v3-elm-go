package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeBrowserImporter struct {
	request posts.BrowserImportRequest
	hashes  []string
	result  posts.BrowserImportResult
	err     error
}

func (f *fakeBrowserImporter) FindBrowserImportDuplicates(_ context.Context, hashes []string) ([]posts.BrowserDuplicate, error) {
	f.hashes = hashes
	return []posts.BrowserDuplicate{{Hash: hashes[0], PostID: "12"}}, f.err
}

func (f *fakeBrowserImporter) ImportBrowser(_ context.Context, request posts.BrowserImportRequest) (posts.BrowserImportResult, error) {
	f.request = request
	return f.result, f.err
}

func TestBrowserImportRoutePassesFieldsAndReturnsDurableResult(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "folder/image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("image"))
	_ = writer.WriteField("tags", "cat, demo")
	_ = writer.WriteField("source", "folder/image.png")
	_ = writer.WriteField("artist", "artist")
	_ = writer.WriteField("duplicate_policy", "skip")
	_ = writer.WriteField("collection_id", "41")
	_ = writer.WriteField("expected_hash", "sha256:"+strings.Repeat("a", 64))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	fake := &fakeBrowserImporter{result: posts.BrowserImportResult{Post: posts.PostDetail{PostSummary: posts.PostSummary{ID: "12"}}, CollectionID: strPtr("41"), Attached: true}}
	request := httptest.NewRequest(http.MethodPost, "/api/imports", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	browserImport(fake).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var got posts.BrowserImportResult
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Post.ID != "12" || !got.Attached || fake.request.Filename != "image.png" || fake.request.CollectionID != "41" || fake.request.Source != "folder/image.png" || fake.request.Artist != "artist" || fake.request.DuplicatePolicy != "skip" || fake.request.ExpectedHash != "sha256:"+strings.Repeat("a", 64) || len(fake.request.Tags) != 1 {
		t.Fatalf("unexpected request/result: request=%#v result=%#v", fake.request, got)
	}
}

func TestBrowserImportRequiresCanonicalExpectedHash(t *testing.T) {
	for _, expected := range []string{"", "sha256:bad", "sha256:" + strings.Repeat("A", 64)} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("file", "image.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte("image"))
		if expected != "" {
			_ = writer.WriteField("expected_hash", expected)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		fake := &fakeBrowserImporter{}
		request := httptest.NewRequest(http.MethodPost, "/api/imports", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		browserImport(fake).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected_hash=%q status=%d", expected, response.Code)
		}
		if fake.request.Filename != "" {
			t.Fatalf("importer called with malformed hash %q", expected)
		}
	}
}

func TestBrowserDuplicateLookupRejectsMalformedAndOversizedLists(t *testing.T) {
	fake := &fakeBrowserImporter{}
	for _, query := range []string{"", "hashes=bad", "hashes=" + strings.Repeat("x,", 100) + "x"} {
		request := httptest.NewRequest(http.MethodGet, "/api/imports/duplicates?"+query, nil)
		response := httptest.NewRecorder()
		browserImportDuplicates(fake).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestBrowserDuplicateLookupReturnsWrapper(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	fake := &fakeBrowserImporter{}
	request := httptest.NewRequest(http.MethodGet, "/api/imports/duplicates?hashes="+hash, nil)
	response := httptest.NewRecorder()
	browserImportDuplicates(fake).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Matches []posts.BrowserDuplicate `json:"matches"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Matches) != 1 || body.Matches[0].Hash != hash || body.Matches[0].PostID != "12" {
		t.Fatalf("matches=%#v", body.Matches)
	}
}

func strPtr(value string) *string { return &value }
