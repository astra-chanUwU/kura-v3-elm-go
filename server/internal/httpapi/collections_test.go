package httpapi

import (
	"bytes"
	"context"
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
