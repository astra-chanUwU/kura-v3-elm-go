package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeSavedSearches struct {
	items   []posts.SavedSearch
	owner   string
	create  posts.CreateSavedSearchRequest
	update  posts.UpdateSavedSearchRequest
	deleted string
}

func (f *fakeSavedSearches) SearchPosts(context.Context, string, *posts.Cursor, int) (posts.SearchPage, error) {
	return posts.SearchPage{}, nil
}
func (f *fakeSavedSearches) ListSavedSearches(_ context.Context, owner string) ([]posts.SavedSearch, error) {
	f.owner = owner
	return f.items, nil
}
func (f *fakeSavedSearches) CreateSavedSearch(_ context.Context, owner string, request posts.CreateSavedSearchRequest) (posts.SavedSearch, error) {
	f.owner, f.create = owner, request
	item := posts.SavedSearch{ID: "1", Name: request.Name, Query: request.Query, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	f.items = append(f.items, item)
	return item, nil
}
func (f *fakeSavedSearches) UpdateSavedSearch(_ context.Context, owner, id string, request posts.UpdateSavedSearchRequest) (posts.SavedSearch, error) {
	f.owner, f.update = owner, request
	return posts.SavedSearch{ID: id, Name: "Updated", Query: "cat"}, nil
}
func (f *fakeSavedSearches) DeleteSavedSearch(_ context.Context, owner, id string) error {
	f.owner, f.deleted = owner, id
	return nil
}

func TestSavedSearchRoutesUseExplicitActorAndValidateKuraQL(t *testing.T) {
	fake := &fakeSavedSearches{}
	router := NewRouter(fake)

	request := httptest.NewRequest(http.MethodPost, "/api/saved-searches", bytes.NewBufferString(`{"name":" Cats ","query":"tag:cat order:score"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || fake.owner != "local" || fake.create.Name != "Cats" {
		t.Fatalf("create: status=%d owner=%q request=%#v body=%s", response.Code, fake.owner, fake.create, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/saved-searches/1", bytes.NewBufferString(`{"query":"order:popular"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid KuraQL status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/saved-searches/1", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || fake.owner != "local" || fake.deleted != "1" {
		t.Fatalf("delete: status=%d owner=%q id=%q", response.Code, fake.owner, fake.deleted)
	}
}

func TestSavedSearchRoutesRequireActorWhenCapabilityTokenConfigured(t *testing.T) {
	router := NewRouterWithToken("secret", &fakeSavedSearches{})
	request := httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing actor status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/saved-searches", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("system actor status=%d body=%s", response.Code, response.Body.String())
	}
}
