package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type tagOnlyBackend struct{ called bool }

func (backend *tagOnlyBackend) EditTags(_ context.Context, _ posts.TagEditRequest) (posts.TagEditResponse, error) {
	backend.called = true
	return posts.TagEditResponse{}, nil
}

func TestExplicitDependenciesAllowIndependentTagCapability(t *testing.T) {
	backend := &tagOnlyBackend{}
	router := NewRouterWithDependencies("secret", Dependencies{Tags: backend})

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/posts/tags", strings.NewReader(`{"posts":[{"id":"1","version":0}],"add":["tag"]}`))
	unauthorizedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized tag edit status=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/posts/tags", strings.NewReader(`{"posts":[{"id":"1","version":0}],"add":["tag"]}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !backend.called {
		t.Fatalf("tag-only backend response status=%d called=%v body=%s", response.Code, backend.called, response.Body.String())
	}

	searchRequest := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	searchResponse := httptest.NewRecorder()
	router.ServeHTTP(searchResponse, searchRequest)
	if searchResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing search capability status=%d body=%s", searchResponse.Code, searchResponse.Body.String())
	}
}

func TestExplicitDependenciesKeepUnavailableRoutesAndMetadata(t *testing.T) {
	router := NewRouterWithDependencies("", Dependencies{})
	for _, route := range []string{"/api/posts", "/api/jobs/42", "/api/collections"} {
		request := httptest.NewRequest(http.MethodGet, route, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s status=%d body=%s", route, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("capability metadata status=%d body=%s", response.Code, response.Body.String())
	}
}
