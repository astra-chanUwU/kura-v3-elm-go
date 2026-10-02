package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

type fakeDetailSearcher struct {
	fakeSearcher
	detail posts.PostDetail
	err    error
	id     string
}

func (f *fakeDetailSearcher) GetPostDetail(_ context.Context, id string) (posts.PostDetail, error) {
	f.id = id
	return f.detail, f.err
}

func TestPostDetailJSONAndErrors(t *testing.T) {
	fake := &fakeDetailSearcher{detail: posts.PostDetail{
		PostSummary: posts.PostSummary{ID: "2004", PreviewURL: "/preview", OriginalURL: "/original", MediaType: "image/jpeg", Width: 679, Height: 437, Tags: []string{"demo", "kson"}},
		Source:      "demo/kson.jpeg", Artist: "Kura Demo", Hash: "sha256:test", FileSize: 165554, CreatedAt: "2026-01-02 00:00:04+00",
	}}
	r := NewRouter(fake)
	resp := requestPath(t, r, "/api/posts/2004")
	if resp.Code != http.StatusOK || fake.id != "2004" {
		t.Fatalf("unexpected detail response: status=%d id=%q", resp.Code, fake.id)
	}
	var got posts.PostDetail
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Source != fake.detail.Source || got.FileSize != fake.detail.FileSize || len(got.Tags) != 2 {
		t.Fatalf("detail JSON lost fields: %#v", got)
	}

	fake.err = posts.ErrNotFound
	if resp := requestPath(t, r, "/api/posts/2004"); resp.Code != http.StatusNotFound {
		t.Fatalf("missing detail status=%d", resp.Code)
	}
	fake.err = posts.ErrUnavailable
	if resp := requestPath(t, r, "/api/posts/2004"); resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable detail status=%d", resp.Code)
	}
	fake.err = errors.New("database exploded")
	if resp := requestPath(t, r, "/api/posts/2004"); resp.Code != http.StatusInternalServerError {
		t.Fatalf("storage detail status=%d", resp.Code)
	}
	if resp := requestPath(t, r, "/api/posts/not-an-id"); resp.Code != http.StatusNotFound {
		t.Fatalf("invalid detail id status=%d", resp.Code)
	}
}

func requestPath(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
