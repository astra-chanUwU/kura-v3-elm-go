package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

type fakeCollectionManager struct {
	renameRequest posts.CollectionManageRequest
	deleteVersion int64
	result        posts.CollectionManageResult
	err           error
}

func (f *fakeCollectionManager) RenameCollection(_ context.Context, id string, request posts.CollectionManageRequest) (posts.CollectionManageResult, error) {
	f.renameRequest = request
	if id != "7" {
		return posts.CollectionManageResult{}, posts.ErrInvalidCollections
	}
	return f.result, f.err
}

func (f *fakeCollectionManager) DeleteCollection(_ context.Context, id string, version int64) (posts.CollectionManageResult, error) {
	f.deleteVersion = version
	if id != "7" {
		return posts.CollectionManageResult{}, posts.ErrInvalidCollections
	}
	return f.result, f.err
}

func runManageHandler(handler http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Patch("/api/collections/{id}", handler)
	r.Delete("/api/collections/{id}", handler)
	request := httptest.NewRequest(method, "/api/collections/7", strings.NewReader(body))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}

func TestRenameCollectionRequiresVersionAndAcceptsZero(t *testing.T) {
	manager := &fakeCollectionManager{result: posts.CollectionManageResult{ID: "7", Name: "New", Version: 1, Changed: true}}
	missing := runManageHandler(RenameCollection(manager), http.MethodPatch, `{"name":"New"}`)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing version status=%d body=%s", missing.Code, missing.Body)
	}
	response := runManageHandler(RenameCollection(manager), http.MethodPatch, `{"name":" New ","expected_version":0}`)
	if response.Code != http.StatusOK || manager.renameRequest.ExpectedVersion != 0 || manager.renameRequest.Name != "New" {
		t.Fatalf("zero version request status=%d request=%#v body=%s", response.Code, manager.renameRequest, response.Body)
	}
	if !strings.Contains(response.Body.String(), `"changed":true`) {
		t.Fatalf("compact result missing: %s", response.Body)
	}
}

func TestDeleteCollectionErrorsAndZeroVersion(t *testing.T) {
	manager := &fakeCollectionManager{result: posts.CollectionManageResult{ID: "7", Name: "Old", Version: 0, Changed: true}}
	missing := runManageHandler(DeleteCollection(manager), http.MethodDelete, `{}`)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing version status=%d", missing.Code)
	}
	manager.err = posts.ErrCollectionChanged
	stale := runManageHandler(DeleteCollection(manager), http.MethodDelete, `{"expected_version":2}`)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"collection_changed"`) {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body)
	}
	manager.err = posts.ErrNotFound
	missingRow := runManageHandler(DeleteCollection(manager), http.MethodDelete, `{"expected_version":0}`)
	if missingRow.Code != http.StatusNotFound {
		t.Fatalf("missing row status=%d", missingRow.Code)
	}
	manager.err = nil
	zero := runManageHandler(DeleteCollection(manager), http.MethodDelete, `{"expected_version":0}`)
	if zero.Code != http.StatusOK || manager.deleteVersion != 0 {
		t.Fatalf("zero version status=%d got=%d", zero.Code, manager.deleteVersion)
	}
}

func TestCollectionManagementCapabilityStaysIndependent(t *testing.T) {
	var _ posts.CollectionMutator = (*oldStyleCollections)(nil)
}
