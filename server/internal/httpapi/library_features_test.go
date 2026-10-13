package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type featureTagReader struct{ calls int }

func (f *featureTagReader) SuggestTags(_ context.Context, prefix string, limit int) ([]posts.TagSuggestion, error) {
	f.calls++
	return []posts.TagSuggestion{{Tag: prefix + "t", Count: 2}}, nil
}

type featureExporter struct {
	calls   int
	request posts.ExportRequest
	err     error
	name    string
}

func (f *featureExporter) ExportLibrary(_ context.Context, r posts.ExportRequest) (*posts.ExportArchive, error) {
	f.calls++
	f.request = r
	if f.err != nil {
		return nil, f.err
	}
	file, err := os.CreateTemp("", "kura-test-export-")
	if err != nil {
		return nil, err
	}
	f.name = file.Name()
	file.WriteString("zip-fixture")
	file.Seek(0, 0)
	return &posts.ExportArchive{File: file, Size: 11}, nil
}

func TestLibraryFeatureRoutes(t *testing.T) {
	tags := &featureTagReader{}
	exports := &featureExporter{}
	router := NewRouterWithDependencies("owner-token", Dependencies{TagSuggestions: tags, Exports: exports})
	response := requestPath(t, router, "/api/tags/suggestions?prefix=ca&limit=12")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"tag":"cat"`) {
		t.Fatalf("suggestions status=%d body=%s", response.Code, response.Body)
	}
	response = requestPath(t, router, "/api/tags/suggestions?limit=0")
	if response.Code != 400 || tags.calls != 1 {
		t.Fatalf("invalid suggestions status=%d calls=%d", response.Code, tags.calls)
	}
	response = requestPath(t, router, "/api/posts/export?ids=7,8&metadata=false")
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/zip" || exports.request.Metadata || response.Body.String() != "zip-fixture" {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body)
	}
	if _, err := os.Stat(exports.name); !os.IsNotExist(err) {
		t.Fatal("archive not removed")
	}
	for _, url := range []string{"/api/posts/export?ids=7,7", "/api/posts/export?ids=01", "/api/collections/7/export", "/api/collections/7/export?expected_version=-1", "/api/posts/export?ids=7&metadata=maybe"} {
		response = requestPath(t, router, url)
		if response.Code != 400 {
			t.Fatalf("invalid %s status=%d", url, response.Code)
		}
	}
	if exports.calls != 1 {
		t.Fatal("invalid request reached exporter")
	}
	exports.err = posts.ErrCollectionChanged
	response = requestPath(t, router, "/api/collections/7/export?expected_version=0")
	if response.Code != 409 || !strings.Contains(response.Body.String(), "collection_changed") {
		t.Fatalf("conflict status=%d", response.Code)
	}
	exports.err = posts.ErrExportTooLarge
	response = requestPath(t, router, "/api/posts/export?ids=7")
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("bounds status=%d", response.Code)
	}
	exports.err = errors.New("internal details")
	response = requestPath(t, router, "/api/posts/export?ids=7")
	if response.Code != 500 || strings.Contains(response.Body.String(), "internal details") {
		t.Fatal("error details leaked")
	}
}

func TestPrivilegedTrashRoutesAndCollectionManagement(t *testing.T) {
	store := &fakeTrashStore{page: posts.TrashPage{Posts: []posts.TrashPost{}}}
	manager := &fakeCollectionManager{result: posts.CollectionManageResult{ID: "7", Name: "New", Version: 1, Changed: true}}
	router := NewRouterWithDependencies("owner-token", Dependencies{Trash: store, CollectionManager: manager})
	response := requestPath(t, router, "/api/posts/trash")
	if response.Code != 401 || store.listCalls != 0 {
		t.Fatal("anonymous trash exposed")
	}
	request := httptest.NewRequest("GET", "/api/posts/trash", nil)
	request.Header.Set("Authorization", "Bearer owner-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || store.listCalls != 1 {
		t.Fatalf("private trash status=%d", response.Code)
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		request = httptest.NewRequest(method, "/api/collections/7", strings.NewReader(`{"expected_version":0,"name":"New"}`))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("ungated management status=%d", response.Code)
		}
	}
}
