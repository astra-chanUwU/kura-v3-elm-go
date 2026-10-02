package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeSearcher struct {
	called bool
	query  string
	cursor *posts.Cursor
	limit  int
}

func (f *fakeSearcher) SearchPosts(_ context.Context, query string, cursor *posts.Cursor, limit int) (posts.SearchPage, error) {
	f.called = true
	f.query, f.cursor, f.limit = query, cursor, limit
	return posts.SearchPage{Posts: []posts.PostSummary{}, NextCursor: nil}, nil
}

func TestSearchPostsValidation(t *testing.T) {
	fake := &fakeSearcher{}
	r := NewRouter(fake)
	for _, raw := range []string{"?limit=0", "?limit=61", "?limit=nope", "?limit="} {
		resp := request(t, r, raw)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("%s: got status %d", raw, resp.Code)
		}
	}
	for _, raw := range []string{"?cursor=bad", "?cursor="} {
		resp := request(t, r, raw)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("%s: got status %d", raw, resp.Code)
		}
	}
	if resp := request(t, r, "?cursor=eyJ2IjoxLCJxIjoiZDU1YjE5M2E0MTE0YzY5ZmE2ZjE5NTAwYzM5ZjQ4ZGNlZjU0MGY1NmM2MmQ1NjI3N2E4MmUzN2I5NzU2M2Y3IiwiaSI6MX0"); resp.Code != http.StatusBadRequest {
		t.Fatalf("empty query cursor: got status %d", resp.Code)
	}
	if resp := request(t, r, "?q=cat&cursor=bad"); resp.Code != http.StatusBadRequest {
		t.Fatalf("mismatched cursor: got status %d", resp.Code)
	}
	if fake.called {
		t.Fatal("invalid requests reached searcher")
	}
}

func TestSearchPostsDefaultsAndEmptyQuery(t *testing.T) {
	fake := &fakeSearcher{}
	r := NewRouter(fake)
	resp := request(t, r, "?q=cat")
	if resp.Code != http.StatusOK || !fake.called || fake.limit != 60 || fake.query != "cat" || fake.cursor != nil {
		t.Fatalf("unexpected first page: status=%d called=%v query=%q limit=%d cursor=%v", resp.Code, fake.called, fake.query, fake.limit, fake.cursor)
	}
	fake.called = false
	resp = request(t, r, "")
	if resp.Code != http.StatusOK || fake.called {
		t.Fatalf("empty query should return empty page: status=%d called=%v", resp.Code, fake.called)
	}
	var body posts.SearchPage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Posts) != 0 || body.NextCursor != nil {
		t.Fatalf("unexpected empty page: %#v", body)
	}
}

func request(t *testing.T, handler http.Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/posts"+query, nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
