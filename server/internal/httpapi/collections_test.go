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

type fakeCollections struct {
	collections   []posts.Collection
	created       posts.CreateCollectionRequest
	added         posts.AddCollectionPostsRequest
	reordered     posts.ReorderCollectionRequest
	removed       bool
	moved         posts.MoveCollectionPostRequest
	moveCalls     int
	moveResult    posts.MoveCollectionPostResult
	moveResultSet bool
	moveErr       error
}

// oldStyleCollections is an actual pre-move backend: it implements the
// original CollectionMutator contract (and Searcher) and deliberately has
// no MoveCollectionPost method and embeds no mover.
type oldStyleCollections struct {
	collections []posts.Collection
	created     posts.CreateCollectionRequest
	added       posts.AddCollectionPostsRequest
	reordered   posts.ReorderCollectionRequest
	removed     bool
}

func (f *oldStyleCollections) SearchPosts(context.Context, string, *posts.Cursor, int) (posts.SearchPage, error) {
	return posts.SearchPage{}, nil
}
func (f *oldStyleCollections) ListCollections(context.Context) ([]posts.Collection, error) {
	return f.collections, nil
}
func (f *oldStyleCollections) CreateCollection(_ context.Context, request posts.CreateCollectionRequest) (posts.Collection, error) {
	f.created = request
	collection := posts.Collection{ID: "1", Name: request.Name, PostIDs: []string{}}
	f.collections = append(f.collections, collection)
	return collection, nil
}
func (f *oldStyleCollections) AddCollectionPosts(_ context.Context, id string, request posts.AddCollectionPostsRequest) (posts.Collection, error) {
	f.added = request
	return posts.Collection{ID: id, Name: "Favorites", PostIDs: request.PostIDs}, nil
}
func (f *oldStyleCollections) RemoveCollectionPost(context.Context, string, string) error {
	f.removed = true
	return nil
}
func (f *oldStyleCollections) ReorderCollection(_ context.Context, id string, request posts.ReorderCollectionRequest) (posts.Collection, error) {
	if _, err := posts.ValidateReorderCollection(request); err != nil {
		return posts.Collection{}, err
	}
	f.reordered = request
	return posts.Collection{ID: id, Name: "Favorites", PostIDs: request.PostIDs}, nil
}

// fakeCollectionPostMover is a standalone mover, independent of any
// CollectionMutator implementation.
type fakeCollectionPostMover struct {
	moved      posts.MoveCollectionPostRequest
	calls      int
	moveResult posts.MoveCollectionPostResult
	resultSet  bool
	moveErr    error
}

func (f *fakeCollectionPostMover) MoveCollectionPost(_ context.Context, id string, request posts.MoveCollectionPostRequest) (posts.MoveCollectionPostResult, error) {
	f.moved = request
	f.calls++
	if f.moveErr != nil {
		return posts.MoveCollectionPostResult{}, f.moveErr
	}
	if f.resultSet {
		return f.moveResult, nil
	}
	return posts.MoveCollectionPostResult{ID: id, CollectionID: id, Version: request.ExpectedVersion + 1, Changed: true}, nil
}

type fakeCollectionPosts struct {
	fakeCollections
	result posts.CollectionPostsPage
	err    error
	id     string
	cursor *posts.CollectionCursor
	limit  int
}

func (f *fakeCollectionPosts) SearchCollectionPosts(_ context.Context, id string, cursor *posts.CollectionCursor, limit int) (posts.CollectionPostsPage, error) {
	f.id, f.cursor, f.limit = id, cursor, limit
	return f.result, f.err
}

func (f *fakeCollections) SearchPosts(context.Context, string, *posts.Cursor, int) (posts.SearchPage, error) {
	return posts.SearchPage{}, nil
}
func (f *fakeCollections) ListCollections(context.Context) ([]posts.Collection, error) {
	return f.collections, nil
}
func (f *fakeCollections) CreateCollection(_ context.Context, request posts.CreateCollectionRequest) (posts.Collection, error) {
	f.created = request
	collection := posts.Collection{ID: "1", Name: request.Name, PostIDs: []string{}}
	f.collections = append(f.collections, collection)
	return collection, nil
}
func (f *fakeCollections) AddCollectionPosts(_ context.Context, id string, request posts.AddCollectionPostsRequest) (posts.Collection, error) {
	f.added = request
	return posts.Collection{ID: id, Name: "Favorites", PostIDs: request.PostIDs}, nil
}
func (f *fakeCollections) RemoveCollectionPost(context.Context, string, string) error {
	f.removed = true
	return nil
}
func (f *fakeCollections) MoveCollectionPost(_ context.Context, id string, request posts.MoveCollectionPostRequest) (posts.MoveCollectionPostResult, error) {
	f.moved = request
	f.moveCalls++
	if f.moveErr != nil {
		return posts.MoveCollectionPostResult{}, f.moveErr
	}
	if f.moveResultSet {
		return f.moveResult, nil
	}
	return posts.MoveCollectionPostResult{ID: id, CollectionID: id, Version: request.ExpectedVersion + 1, Changed: true}, nil
}
func (f *fakeCollections) ReorderCollection(_ context.Context, id string, request posts.ReorderCollectionRequest) (posts.Collection, error) {
	if _, err := posts.ValidateReorderCollection(request); err != nil {
		return posts.Collection{}, err
	}
	// validate collection id format like real mutator
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			return posts.Collection{}, posts.ErrInvalidCollections
		}
	}
	if id == "" || id == "0" {
		return posts.Collection{}, posts.ErrInvalidCollections
	}
	f.reordered = request
	return posts.Collection{ID: id, Name: "Favorites", PostIDs: request.PostIDs}, nil
}

func TestCollectionRoutes(t *testing.T) {
	fake := &fakeCollections{}
	router := NewRouter(fake)
	request := httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"  Favorites "}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || fake.created.Name != "Favorites" {
		t.Fatalf("create: status=%d request=%#v", response.Code, fake.created)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/collections/1/posts", bytes.NewBufferString(`{"post_ids":["10","11"]}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(fake.added.PostIDs) != 2 {
		t.Fatalf("add: status=%d request=%#v", response.Code, fake.added)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/collections/1/posts/10", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !fake.removed {
		t.Fatalf("remove: status=%d removed=%v", response.Code, fake.removed)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":""}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d", response.Code)
	}
}

func TestCollectionPostsRoute(t *testing.T) {
	fake := &fakeCollectionPosts{result: posts.CollectionPostsPage{Posts: []posts.PostSummary{{ID: "11"}, {ID: "10"}}, CollectionVersion: 3}}
	router := NewRouter(fake)
	response := requestPath(t, router, "/api/collections/7/posts?limit=2")
	if response.Code != http.StatusOK || fake.id != "7" || fake.limit != 2 {
		t.Fatalf("browse: status=%d id=%q limit=%d", response.Code, fake.id, fake.limit)
	}
	if fake.cursor != nil {
		t.Fatalf("first page sent cursor=%#v", fake.cursor)
	}
	body := response.Body.Bytes()
	var got posts.CollectionPostsPage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 2 || got.Posts[0].ID != "11" || got.NextCursor != nil {
		t.Fatalf("unexpected collection page: %#v", got)
	}
	if got.CollectionVersion != 3 {
		t.Fatalf("collection version=%d want 3", got.CollectionVersion)
	}
	// The envelope stays additive: a legacy SearchPage decoder still reads
	// the same posts and cursor from a versioned response.
	var legacy posts.SearchPage
	if err := json.Unmarshal(body, &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if len(legacy.Posts) != 2 || legacy.Posts[0].ID != "11" || legacy.NextCursor != nil {
		t.Fatalf("legacy view diverged: %#v", legacy)
	}
	for _, path := range []string{"/api/collections/7/posts?limit=0", "/api/collections/7/posts?limit=61"} {
		if response := requestPath(t, router, path); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d", path, response.Code)
		}
	}
	fake.err = posts.ErrNotFound
	if response := requestPath(t, router, "/api/collections/7/posts"); response.Code != http.StatusNotFound {
		t.Fatalf("missing collection status=%d", response.Code)
	}
}

func TestCollectionPostsCursorValidation(t *testing.T) {
	fake := &fakeCollectionPosts{result: posts.CollectionPostsPage{Posts: []posts.PostSummary{{ID: "11"}}}}
	router := NewRouter(fake)
	for _, path := range []string{
		"/api/collections/7/posts?cursor=bad",
		"/api/collections/7/posts?cursor=",
	} {
		if response := requestPath(t, router, path); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d want 400", path, response.Code)
		}
	}
	other, err := posts.EncodeCollectionCursor(8, 0, 0, 11)
	if err != nil {
		t.Fatal(err)
	}
	if response := requestPath(t, router, "/api/collections/7/posts?cursor="+other); response.Code != http.StatusBadRequest {
		t.Fatalf("cross-collection cursor status=%d want 400", response.Code)
	}
	if fake.id != "" {
		t.Fatalf("invalid cursors reached searcher for collection %q", fake.id)
	}
	valid, err := posts.EncodeCollectionCursor(7, 4, 0, 11)
	if err != nil {
		t.Fatal(err)
	}
	fake.id = ""
	if response := requestPath(t, router, "/api/collections/7/posts?cursor="+valid); response.Code != http.StatusOK {
		t.Fatalf("valid cursor status=%d want 200", response.Code)
	}
	if fake.cursor == nil || fake.cursor.CollectionID != 7 || fake.cursor.Version != 4 || fake.cursor.PostID != 11 {
		t.Fatalf("cursor not forwarded: %#v", fake.cursor)
	}
}

func TestCollectionPostsConflict(t *testing.T) {
	fake := &fakeCollectionPosts{err: posts.ErrCollectionChanged}
	router := NewRouter(fake)
	valid, err := posts.EncodeCollectionCursor(7, 4, 0, 11)
	if err != nil {
		t.Fatal(err)
	}
	response := requestPath(t, router, "/api/collections/7/posts?cursor="+valid)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale version status=%d want 409", response.Code)
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "collection changed" || body.Code != "collection_changed" {
		t.Fatalf("unexpected conflict body: %#v", body)
	}
}

func TestReorderCollectionRoute(t *testing.T) {
	fake := &fakeCollections{collections: []posts.Collection{{ID: "1", Name: "Favorites", PostIDs: []string{"10", "11", "12"}}}}
	router := NewRouter(fake)
	request := httptest.NewRequest(http.MethodPost, "/api/collections/1/order", bytes.NewBufferString(`{"post_ids":["12","10","11"]}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(fake.reordered.PostIDs) != 3 || fake.reordered.PostIDs[0] != "12" {
		t.Fatalf("reorder: status=%d reordered=%#v", response.Code, fake.reordered)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/collections/1/order", bytes.NewBufferString(`{"post_ids":["10","10"]}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid reorder duplicate status=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/collections/bad/order", bytes.NewBufferString(`{"post_ids":["10"]}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid collection id status=%d", response.Code)
	}
	over := `{"post_ids":[`
	for i := 1; i <= 201; i++ {
		if i > 1 {
			over += ","
		}
		over += `"` + string(rune('0'+i%10)) + `"`
	}
	over += "]}"
	_ = over
	request = httptest.NewRequest(http.MethodPost, "/api/collections/1/order", bytes.NewBufferString(`{"post_ids":["bad"]}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid post id status=%d", response.Code)
	}
}

func TestMoveCollectionPostRoute(t *testing.T) {
	fake := &fakeCollections{}
	router := NewRouter(fake)
	request := httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":"12","expected_version":3}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("move: status=%d body=%s", response.Code, response.Body.String())
	}
	if fake.moved.PostID != "10" || fake.moved.BeforePostID == nil || *fake.moved.BeforePostID != "12" || fake.moved.ExpectedVersion != 3 {
		t.Fatalf("move request not forwarded: %#v", fake.moved)
	}
	var got posts.MoveCollectionPostResult
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CollectionID != "7" || got.Version != 4 || !got.Changed {
		t.Fatalf("unexpected move result: %#v", got)
	}
	// Compact: no member list is echoed.
	var raw map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["post_ids"]; ok {
		t.Fatalf("move response echoed post_ids: %v", raw)
	}

	// Null neighbor moves to the end; no-op results stay version-stable.
	fake.moveResultSet = true
	fake.moveResult = posts.MoveCollectionPostResult{ID: "7", CollectionID: "7", Version: 3, Changed: false}
	request = httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":3}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fake.moved.BeforePostID != nil {
		t.Fatalf("move to end: status=%d moved=%#v", response.Code, fake.moved)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || got.Changed {
		t.Fatalf("no-op move result: %#v", got)
	}
	fake.moveResultSet = false

	for _, body := range []string{
		`{"post_id":"0","before_post_id":null,"expected_version":0}`,
		`{"post_id":"10","before_post_id":"bad","expected_version":0}`,
		`{"post_id":"10","before_post_id":null,"expected_version":-1}`,
		`{"post_id":"","before_post_id":null,"expected_version":0}`,
		`{"post_id":"10","before_post_id":null,"expected_version":0,"extra":1}`,
	} {
		request = httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(body))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s: status=%d want 400", body, response.Code)
		}
	}

	fake.moveErr = posts.ErrCollectionChanged
	request = httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":0}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale move status=%d want 409", response.Code)
	}
	var conflict struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Code != "collection_changed" {
		t.Fatalf("stale move body: %#v", conflict)
	}

	fake.moveErr = posts.ErrNotFound
	request = httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":"99","expected_version":0}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing neighbor status=%d want 404", response.Code)
	}
	fake.moveErr = posts.ErrInvalidCollections
	request = httptest.NewRequest(http.MethodPost, "/api/collections/bad/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":0}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid collection move status=%d want 400", response.Code)
	}
}

func TestOldStyleCollectionBackendKeepsRoutesAndMoveUnavailable(t *testing.T) {
	backend := &oldStyleCollections{}
	// The old-style backend satisfies the original contracts but not the mover.
	var _ posts.CollectionMutator = backend
	var _ posts.Searcher = backend
	if _, ok := any(backend).(posts.CollectionPostMover); ok {
		t.Fatal("old-style backend unexpectedly implements CollectionPostMover")
	}
	for _, router := range []http.Handler{
		NewRouter(backend),
		NewRouterWithDependencies("", Dependencies{Collections: backend}),
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/collections", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("list collections: status=%d body=%s", response.Code, response.Body.String())
		}
		request = httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Favorites"}`))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create collection: status=%d body=%s", response.Code, response.Body.String())
		}
		request = httptest.NewRequest(http.MethodPost, "/api/collections/1/posts", bytes.NewBufferString(`{"post_ids":["10","11"]}`))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("add posts: status=%d body=%s", response.Code, response.Body.String())
		}
		request = httptest.NewRequest(http.MethodPost, "/api/collections/1/order", bytes.NewBufferString(`{"post_ids":["11","10"]}`))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("reorder: status=%d body=%s", response.Code, response.Body.String())
		}
		request = httptest.NewRequest(http.MethodDelete, "/api/collections/1/posts/10", nil)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("remove: status=%d body=%s", response.Code, response.Body.String())
		}
		request = httptest.NewRequest(http.MethodPost, "/api/collections/1/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":0}`))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("move on old backend: status=%d want 503 body=%s", response.Code, response.Body.String())
		}
	}
}

func TestMoveCollectionPostIndependentMoverDependency(t *testing.T) {
	backend := &oldStyleCollections{}
	mover := &fakeCollectionPostMover{}
	router := NewRouterWithDependencies("", Dependencies{Collections: backend, CollectionPostMover: mover})
	request := httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":0}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || mover.calls != 1 {
		t.Fatalf("independent mover: status=%d calls=%d body=%s", response.Code, mover.calls, response.Body.String())
	}
}

func TestMoveCollectionPostPresence(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		want     int
		wantCall bool
	}{
		{"explicit null neighbor and explicit zero version", `{"post_id":"10","before_post_id":null,"expected_version":0}`, http.StatusOK, true},
		{"valid string neighbor", `{"post_id":"10","before_post_id":"12","expected_version":0}`, http.StatusOK, true},
		{"invalid type neighbor number", `{"post_id":"10","before_post_id":123,"expected_version":0}`, http.StatusBadRequest, false},
		{"invalid type neighbor bool", `{"post_id":"10","before_post_id":true,"expected_version":0}`, http.StatusBadRequest, false},
		{"invalid type neighbor object", `{"post_id":"10","before_post_id":{},"expected_version":0}`, http.StatusBadRequest, false},
		{"invalid type neighbor array", `{"post_id":"10","before_post_id":[],"expected_version":0}`, http.StatusBadRequest, false},
		{"missing neighbor", `{"post_id":"10","expected_version":0}`, http.StatusBadRequest, false},
		{"missing expected_version", `{"post_id":"10","before_post_id":null}`, http.StatusBadRequest, false},
		{"null expected_version", `{"post_id":"10","before_post_id":null,"expected_version":null}`, http.StatusBadRequest, false},
		{"invalid type expected_version string", `{"post_id":"10","before_post_id":null,"expected_version":"0"}`, http.StatusBadRequest, false},
		{"invalid type expected_version float", `{"post_id":"10","before_post_id":null,"expected_version":1.5}`, http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeCollections{}
			router := NewRouter(fake)
			request := httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(tc.body))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want %d body=%s", response.Code, tc.want, response.Body.String())
			}
			called := fake.moveCalls > 0
			if called != tc.wantCall {
				t.Fatalf("mover called=%v want %v (moveCalls=%d)", called, tc.wantCall, fake.moveCalls)
			}
			if tc.want == http.StatusOK && tc.name == "explicit null neighbor and explicit zero version" {
				if fake.moved.BeforePostID != nil || fake.moved.ExpectedVersion != 0 {
					t.Fatalf("explicit null/zero not preserved: %#v", fake.moved)
				}
			}
			if tc.want == http.StatusOK && tc.name == "valid string neighbor" {
				if fake.moved.BeforePostID == nil || *fake.moved.BeforePostID != "12" {
					t.Fatalf("string neighbor not forwarded: %#v", fake.moved)
				}
			}
		})
	}
}

func TestMoveCollectionPostRequiresWriteCapability(t *testing.T) {
	fake := &fakeCollections{}
	router := NewRouterWithToken("move-token", fake)
	request := httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":0}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated move status=%d want 401", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/collections/7/move", bytes.NewBufferString(`{"post_id":"10","before_post_id":null,"expected_version":0}`))
	request.Header.Set("Authorization", "Bearer move-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated move status=%d want 200", response.Code)
	}
}
