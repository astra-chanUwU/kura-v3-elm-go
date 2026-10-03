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
	collections []posts.Collection
	created     posts.CreateCollectionRequest
	added       posts.AddCollectionPostsRequest
	reordered   posts.ReorderCollectionRequest
	removed     bool
}

type fakeCollectionPosts struct {
	fakeCollections
	result posts.SearchPage
	err    error
	id     string
	limit  int
}

func (f *fakeCollectionPosts) SearchCollectionPosts(_ context.Context, id string, limit int) (posts.SearchPage, error) {
	f.id, f.limit = id, limit
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
	fake := &fakeCollectionPosts{result: posts.SearchPage{Posts: []posts.PostSummary{{ID: "11"}, {ID: "10"}}}}
	router := NewRouter(fake)
	response := requestPath(t, router, "/api/collections/7/posts?limit=2")
	if response.Code != http.StatusOK || fake.id != "7" || fake.limit != 2 {
		t.Fatalf("browse: status=%d id=%q limit=%d", response.Code, fake.id, fake.limit)
	}
	var got posts.SearchPage
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 2 || got.Posts[0].ID != "11" || got.NextCursor != nil {
		t.Fatalf("unexpected collection page: %#v", got)
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
