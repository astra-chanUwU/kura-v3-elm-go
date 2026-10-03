package httpapi

import (
	"bytes"
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
	result posts.SearchPage
}

func (f *fakeSearcher) SearchPosts(_ context.Context, query string, cursor *posts.Cursor, limit int) (posts.SearchPage, error) {
	f.called = true
	f.query, f.cursor, f.limit = query, cursor, limit
	return f.result, nil
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
	if resp := request(t, r, "?q=cat&cursor=bad"); resp.Code != http.StatusBadRequest {
		t.Fatalf("mismatched cursor: got status %d", resp.Code)
	}
	catCursor, err := posts.EncodeCursor("cat", 1)
	if err != nil {
		t.Fatal(err)
	}
	if resp := request(t, r, "?q=dog&cursor="+catCursor); resp.Code != http.StatusBadRequest {
		t.Fatalf("query-mismatched cursor: got status %d", resp.Code)
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
	next, err := posts.EncodeCursor("", 42)
	if err != nil {
		t.Fatal(err)
	}
	fake.result = posts.SearchPage{
		Posts:      []posts.PostSummary{{ID: "42"}},
		NextCursor: &next,
	}
	resp = request(t, r, "")
	if resp.Code != http.StatusOK || !fake.called || fake.query != "" || fake.limit != 60 || fake.cursor != nil {
		t.Fatalf("empty query should reach searcher: status=%d called=%v query=%q limit=%d cursor=%v", resp.Code, fake.called, fake.query, fake.limit, fake.cursor)
	}
	var body posts.SearchPage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Posts) != 1 || body.Posts[0].ID != "42" || body.NextCursor == nil || *body.NextCursor != next {
		t.Fatalf("unexpected empty page: %#v", body)
	}
	validEmptyCursor, err := posts.EncodeCursor("", 42)
	if err != nil {
		t.Fatal(err)
	}
	fake.called = false
	resp = request(t, r, "?cursor="+validEmptyCursor)
	if resp.Code != http.StatusOK || !fake.called || fake.cursor == nil || fake.cursor.ID != 42 {
		t.Fatalf("valid empty-query cursor rejected: status=%d called=%v cursor=%v", resp.Code, fake.called, fake.cursor)
	}
}

func TestSearchPostsRejectsInvalidKuraQuery(t *testing.T) {
	fake := &fakeSearcher{}
	r := NewRouter(fake)
	for _, raw := range []string{"?q=order%3Ascore", "?q=score%3A%3E%3D", "?q=favorite%3Amaybe"} {
		resp := request(t, r, raw)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("%s: got status %d", raw, resp.Code)
		}
	}
	if fake.called {
		t.Fatal("invalid query reached searcher")
	}
}

func TestSearchPostsUnavailableWithoutSearcher(t *testing.T) {
	if resp := request(t, NewRouter(), ""); resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty query without searcher: got status %d", resp.Code)
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

type fakeTagMutator struct {
	fakeSearcher
	result posts.TagEditResponse
	err    error
	got    posts.TagEditRequest
}

type fakeTagReverter struct {
	fakeSearcher
	result posts.TagRevertResponse
	err    error
	got    posts.TagRevertRequest
}

func (f *fakeTagReverter) RevertTags(_ context.Context, request posts.TagRevertRequest) (posts.TagRevertResponse, error) {
	f.got = request
	return f.result, f.err
}

type fakeReactionMutator struct {
	fakeSearcher
	result posts.ReactionResponse
	err    error
	got    posts.ReactionRequest
}

func (f *fakeReactionMutator) EditReactions(_ context.Context, request posts.ReactionRequest) (posts.ReactionResponse, error) {
	f.got = request
	return f.result, f.err
}

func (f *fakeTagMutator) EditTags(_ context.Context, request posts.TagEditRequest) (posts.TagEditResponse, error) {
	f.got = request
	return f.result, f.err
}

func TestTagEditRouteValidationAndErrors(t *testing.T) {
	fake := &fakeTagMutator{result: posts.TagEditResponse{Posts: []posts.TagEditResult{{ID: "2004", Version: 1, Tags: []string{"night"}, Changed: true}}}}
	r := NewRouter(fake)
	req := httptest.NewRequest(http.MethodPost, "/api/posts/tags", bytes.NewBufferString(`{"posts":[{"id":"2004","version":0}],"add":[" night "],"remove":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || len(fake.got.Add) != 1 || fake.got.Add[0] != "night" {
		t.Fatalf("unexpected tag edit response: status=%d request=%#v", resp.Code, fake.got)
	}

	for _, body := range []string{
		`{"posts":[],"add":["x"]}`,
		`{"posts":[{"id":"2004","version":0}],"add":["x","x"]}`,
		`{"posts":[{"id":"2004","version":0}],"add":["x"],"remove":["x"]}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/posts/tags", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s: got status %d", body, response.Code)
		}
	}

	fake.err = posts.ErrConflict
	request := httptest.NewRequest(http.MethodPost, "/api/posts/tags", bytes.NewBufferString(`{"posts":[{"id":"2004","version":0}],"add":["x"]}`))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d", response.Code)
	}
}

func TestTagRevertRouteValidationAndErrors(t *testing.T) {
	fake := &fakeTagReverter{result: posts.TagRevertResponse{Posts: []posts.TagEditResult{{ID: "2004", Version: 3, Tags: []string{"night"}, Changed: true}}}}
	r := NewRouter(fake)
	request := httptest.NewRequest(http.MethodPost, "/api/posts/tags/revert", bytes.NewBufferString(`{"posts":[{"id":"2004","version":2}],"target_version":1}`))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fake.got.TargetVersion != 1 || len(fake.got.Posts) != 1 {
		t.Fatalf("unexpected revert response: status=%d request=%#v", response.Code, fake.got)
	}
	for _, body := range []string{
		`{"posts":[],"target_version":1}`,
		`{"posts":[{"id":"2004","version":0}],"target_version":0}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/posts/tags/revert", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s: got status %d", body, response.Code)
		}
	}
	fake.err = posts.ErrConflict
	request = httptest.NewRequest(http.MethodPost, "/api/posts/tags/revert", bytes.NewBufferString(`{"posts":[{"id":"2004","version":2}],"target_version":1}`))
	response = httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d", response.Code)
	}
}

func TestReactionRouteValidationAndErrors(t *testing.T) {
	favorite := true
	fake := &fakeReactionMutator{result: posts.ReactionResponse{Posts: []posts.ReactionResult{{ID: "2004", Version: 1, Favorite: true, Score: 2, Changed: true}}}}
	r := NewRouter(fake)
	req := httptest.NewRequest(http.MethodPost, "/api/posts/reactions", bytes.NewBufferString(`{"posts":[{"id":"2004","version":0}],"favorite":true,"score":2}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || fake.got.Favorite == nil || *fake.got.Favorite != favorite || fake.got.Score == nil || *fake.got.Score != 2 {
		t.Fatalf("unexpected reaction response: status=%d request=%#v", resp.Code, fake.got)
	}
	for _, body := range []string{
		`{"posts":[],"favorite":true}`,
		`{"posts":[{"id":"2004","version":0}]}`,
		`{"posts":[{"id":"2004","version":0}],"score":-1}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/posts/reactions", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s: got status %d", body, response.Code)
		}
	}
	fake.err = posts.ErrReactionConflict
	request := httptest.NewRequest(http.MethodPost, "/api/posts/reactions", bytes.NewBufferString(`{"posts":[{"id":"2004","version":0}],"favorite":false}`))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d", response.Code)
	}
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
