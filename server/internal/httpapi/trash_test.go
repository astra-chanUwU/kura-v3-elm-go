package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeTrashStore struct {
	page       posts.TrashPage
	response   posts.TrashResponse
	err        error
	actor      string
	limit      int
	cursor     *posts.TrashCursor
	request    posts.TrashRequest
	listCalls  int
	trashCalls int
	restCalls  int
}

func (f *fakeTrashStore) ListTrash(_ context.Context, actor string, limit int, cursor *posts.TrashCursor) (posts.TrashPage, error) {
	f.listCalls++
	f.actor, f.limit, f.cursor = actor, limit, cursor
	return f.page, f.err
}

func (f *fakeTrashStore) Trash(_ context.Context, actor string, request posts.TrashRequest) (posts.TrashResponse, error) {
	f.trashCalls++
	f.actor, f.request = actor, request
	return f.response, f.err
}

func (f *fakeTrashStore) Restore(_ context.Context, actor string, request posts.TrashRequest) (posts.TrashResponse, error) {
	f.restCalls++
	f.actor, f.request = actor, request
	return f.response, f.err
}

func authorizedRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	return withActor(request, Actor{ID: "owner-1"})
}

func TestListTrashRequiresCapabilityAndValidatesQuery(t *testing.T) {
	fake := &fakeTrashStore{page: posts.TrashPage{Posts: []posts.TrashPost{{PostSummary: posts.PostSummary{ID: "7"}, DeletedAt: "2026-10-01T00:00:00Z"}}}}
	handler := listTrash(fake)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/posts/trash", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest(http.MethodGet, "/api/posts/trash?limit=10", ""))
	if response.Code != http.StatusOK || fake.actor != "owner-1" || fake.limit != 10 || fake.listCalls != 1 {
		t.Fatalf("list status=%d actor=%q limit=%d calls=%d body=%s", response.Code, fake.actor, fake.limit, fake.listCalls, response.Body.String())
	}
	for _, target := range []string{
		"/api/posts/trash?limit=0",
		"/api/posts/trash?limit=1&limit=2",
		"/api/posts/trash?other=1",
		"/api/posts/trash?cursor=broken",
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedRequest(http.MethodGet, target, ""))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestTrashAndRestoreValidateBeforeStoreWrites(t *testing.T) {
	fake := &fakeTrashStore{response: posts.TrashResponse{Posts: []posts.TrashResult{{ID: "7", Changed: true}}}}
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		body    string
		want    int
	}{
		{"trash", trashPosts(fake), `{"post_ids":["7"],"reason":"cleanup"}`, http.StatusOK},
		{"restore", restorePosts(fake), `{"post_ids":["7"]}`, http.StatusOK},
		{"invalid canonical id", trashPosts(fake), `{"post_ids":["07"]}`, http.StatusBadRequest},
		{"duplicate", restorePosts(fake), `{"post_ids":["7","7"]}`, http.StatusBadRequest},
		{"unknown field", trashPosts(fake), `{"post_ids":["7"],"force":true}`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.handler.ServeHTTP(response, authorizedRequest(http.MethodPost, "/api/posts/trash", test.body))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
	if fake.trashCalls != 1 || fake.restCalls != 1 || fake.actor != "owner-1" || fake.request.Reason != "" {
		t.Fatalf("unexpected writes: trash=%d restore=%d actor=%q request=%#v", fake.trashCalls, fake.restCalls, fake.actor, fake.request)
	}
	var decoded posts.TrashResponse
	if err := json.Unmarshal([]byte(`{"posts":[{"id":"7","changed":true}]}`), &decoded); err != nil || len(decoded.Posts) != 1 {
		t.Fatalf("response contract did not decode: %#v err=%v", decoded, err)
	}
}
