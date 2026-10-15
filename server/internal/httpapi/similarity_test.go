package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

type fakeSimilarityReader struct {
	page     posts.SimilarPage
	err      error
	anchor   string
	distance int
	limit    int
	cursor   string
	calls    int
}

func (f *fakeSimilarityReader) ListSimilarPosts(_ context.Context, anchorID string, maxDistance, limit int, cursor string) (posts.SimilarPage, error) {
	f.calls++
	f.anchor, f.distance, f.limit, f.cursor = anchorID, maxDistance, limit, cursor
	return f.page, f.err
}

type fakeSimilarityIndexer struct {
	indexResponse posts.SimilarityIndexResponse
	indexErr      error
	statusPage    posts.SimilarityStatusPage
	statusErr     error
	indexIDs      []string
	statusLimit   int
	statusCursor  string
	indexCalls    int
	statusCalls   int
}

func (f *fakeSimilarityIndexer) IndexSimilarityPosts(_ context.Context, ids []string) (posts.SimilarityIndexResponse, error) {
	f.indexCalls++
	f.indexIDs = append([]string(nil), ids...)
	return f.indexResponse, f.indexErr
}

func (f *fakeSimilarityIndexer) GetSimilarityStatus(_ context.Context, limit int, cursor string) (posts.SimilarityStatusPage, error) {
	f.statusCalls++
	f.statusLimit, f.statusCursor = limit, cursor
	return f.statusPage, f.statusErr
}

func similarRouter(reader posts.SimilarityReader) http.Handler {
	router := chi.NewRouter()
	router.Get("/api/posts/{id}/similar", getSimilarPosts(reader))
	return router
}

func TestGetSimilarPostsServesPublicPage(t *testing.T) {
	fake := &fakeSimilarityReader{page: posts.SimilarPage{
		PostID:    "7",
		Algorithm: posts.SimilarityAlgorithm,
		Indexed:   true,
		Matches: []posts.SimilarMatch{{
			Post:     posts.PostSummary{ID: "9", PreviewURL: "/media/a.jpg", OriginalURL: "/media/b.jpg", MediaType: "image/jpeg", Width: 64, Height: 48, Tags: []string{}},
			Distance: 3,
		}},
	}}
	router := similarRouter(fake)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/posts/7/similar?distance=8&limit=30", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if fake.calls != 1 || fake.anchor != "7" || fake.distance != 8 || fake.limit != 30 {
		t.Fatalf("reader args anchor=%q distance=%d limit=%d calls=%d", fake.anchor, fake.distance, fake.limit, fake.calls)
	}
	body := response.Body.String()
	for _, leaked := range []string{"sha256", "job_id", "fingerprint", "original_hash"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public response leaks %q: %s", leaked, body)
		}
	}
	var decoded posts.SimilarPage
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response not a SimilarPage: %v", err)
	}
	if decoded.Algorithm != "dhash-v1" || len(decoded.Matches) != 1 || decoded.Matches[0].Distance != 3 {
		t.Fatalf("page=%+v", decoded)
	}
}

func TestGetSimilarPostsDefaultsAndErrors(t *testing.T) {
	fake := &fakeSimilarityReader{page: posts.SimilarPage{PostID: "7", Algorithm: posts.SimilarityAlgorithm, Matches: []posts.SimilarMatch{}}}
	router := similarRouter(fake)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/posts/7/similar", nil))
	if response.Code != http.StatusOK || fake.distance != posts.DefaultSimilarDistance || fake.limit != posts.DefaultSimilarLimit {
		t.Fatalf("defaults status=%d distance=%d limit=%d body=%s", response.Code, fake.distance, fake.limit, response.Body.String())
	}
	for _, target := range []string{
		"/api/posts/0/similar",
		"/api/posts/abc/similar",
		"/api/posts/01/similar",
	} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status=%d body=%s (bad ids read as missing)", target, response.Code, response.Body.String())
		}
	}
	for _, target := range []string{
		"/api/posts/7/similar?distance=17",
		"/api/posts/7/similar?distance=-1",
		"/api/posts/7/similar?distance=x",
		"/api/posts/7/similar?limit=0",
		"/api/posts/7/similar?limit=61",
		"/api/posts/7/similar?cursor=",
	} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestGetSimilarPostsMapsBackendErrors(t *testing.T) {
	for _, mapping := range []struct {
		err    error
		status int
	}{
		{posts.ErrNotFound, http.StatusNotFound},
		{posts.ErrInvalidCursor, http.StatusBadRequest},
		{posts.ErrInvalidQuery, http.StatusBadRequest},
		{posts.ErrUnavailable, http.StatusServiceUnavailable},
		{errors.New("boom"), http.StatusInternalServerError},
	} {
		fake := &fakeSimilarityReader{err: mapping.err}
		response := httptest.NewRecorder()
		similarRouter(fake).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/posts/7/similar", nil))
		if response.Code != mapping.status {
			t.Errorf("err=%v status=%d want %d body=%s", mapping.err, response.Code, mapping.status, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	similarRouter(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/posts/7/similar", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil reader status=%d", response.Code)
	}
}

func TestPostSimilarityIndexRequiresCapabilityAndValidates(t *testing.T) {
	fake := &fakeSimilarityIndexer{indexResponse: posts.SimilarityIndexResponse{Posts: []posts.SimilarityIndexResult{{ID: "7", JobID: "11", Status: "pending"}}}}
	handler := postSimilarityIndex(fake)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/posts/similarity/index", strings.NewReader(`{"post_ids":["7"]}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.Code)
	}
	for _, body := range []string{
		`{}`,
		`{"post_ids":[]}`,
		`{"post_ids":["1","1"]}`,
		`{"post_ids":["abc"]}`,
		`{"post_ids":["7"],"extra":1}`,
		`not json`,
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedRequest(http.MethodPost, "/api/posts/similarity/index", body))
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %q status=%d", body, response.Code)
		}
	}
	if fake.indexCalls != 0 {
		t.Fatalf("invalid batches reached the backend: %d calls", fake.indexCalls)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest(http.MethodPost, "/api/posts/similarity/index", `{"post_ids":["7"]}`))
	if response.Code != http.StatusOK || fake.indexCalls != 1 || len(fake.indexIDs) != 1 || fake.indexIDs[0] != "7" {
		t.Fatalf("status=%d calls=%d ids=%v body=%s", response.Code, fake.indexCalls, fake.indexIDs, response.Body.String())
	}
	fake.indexErr = posts.ErrNotFound
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest(http.MethodPost, "/api/posts/similarity/index", `{"post_ids":["7"]}`))
	if response.Code != http.StatusNotFound {
		t.Fatalf("not-found status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetSimilarityStatusIsProtectedAndClean(t *testing.T) {
	fake := &fakeSimilarityIndexer{statusPage: posts.SimilarityStatusPage{
		Algorithm:     posts.SimilarityAlgorithm,
		IndexedCount:  1,
		EligibleCount: 2,
		Posts: []posts.SimilarityStatusItem{
			{ID: "7", Indexed: true, JobID: strptr("11"), Status: strptr("succeeded")},
			{ID: "9", Indexed: false},
		},
	}}
	handler := getSimilarityStatus(fake)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/posts/similarity/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status read status=%d (GET must check the actor explicitly)", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest(http.MethodGet, "/api/posts/similarity/status?limit=100", ""))
	if response.Code != http.StatusOK || fake.statusCalls != 1 || fake.statusLimit != 100 {
		t.Fatalf("status=%d calls=%d limit=%d body=%s", response.Code, fake.statusCalls, fake.statusLimit, response.Body.String())
	}
	body := response.Body.String()
	for _, leaked := range []string{"preview_url", "original_url", "sha256", "fingerprint"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("status response leaks %q: %s", leaked, body)
		}
	}
	for _, target := range []string{
		"/api/posts/similarity/status?limit=0",
		"/api/posts/similarity/status?limit=101",
		"/api/posts/similarity/status?cursor=",
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedRequest(http.MethodGet, target, ""))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d", target, response.Code)
		}
	}
	fake.statusErr = posts.ErrInvalidCursor
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest(http.MethodGet, "/api/posts/similarity/status?cursor=broken", ""))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor status=%d", response.Code)
	}
}

func strptr(value string) *string {
	return &value
}
