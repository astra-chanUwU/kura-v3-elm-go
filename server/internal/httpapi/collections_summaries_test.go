package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeCollectionSummaryReader struct {
	summaries []posts.CollectionSummary
	err       error
	calls     int
}

func (f *fakeCollectionSummaryReader) ListCollectionSummaries(context.Context) ([]posts.CollectionSummary, error) {
	f.calls++
	return f.summaries, f.err
}

// legacySummaryBackend is a Searcher backend that also implements the
// independent summary capability, for legacy discovery tests.
type legacySummaryBackend struct {
	*oldStyleCollections
	summaries []posts.CollectionSummary
}

func (b *legacySummaryBackend) ListCollectionSummaries(context.Context) ([]posts.CollectionSummary, error) {
	return b.summaries, nil
}

func TestCollectionSummariesRoute(t *testing.T) {
	reader := &fakeCollectionSummaryReader{summaries: []posts.CollectionSummary{
		{ID: "1", Name: "Empty", Version: 0, MemberCount: 0, VisiblePostCount: 0},
		{ID: "2", Name: "Huge", Version: 7, MemberCount: 1000, VisiblePostCount: 998},
	}}
	router := NewRouterWithDependencies("", Dependencies{CollectionSummaries: reader})
	response := requestPath(t, router, "/api/collections/summaries")
	if response.Code != http.StatusOK || reader.calls != 1 {
		t.Fatalf("summaries: status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "post_ids") || strings.Contains(body, `"posts"`) {
		t.Fatalf("summaries leaked member data: %s", body)
	}
	// Bounded cost for the 1000-member collection: no per-member payload.
	if len(body) > 500 {
		t.Fatalf("summaries body too large (%d bytes): %s", len(body), body)
	}
	var got struct {
		Collections []posts.CollectionSummary `json:"collections"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Collections) != 2 || got.Collections[0].Name != "Empty" || got.Collections[1].MemberCount != 1000 || got.Collections[1].VisiblePostCount != 998 || got.Collections[1].Version != 7 {
		t.Fatalf("unexpected summaries: %#v", got)
	}
	// Exact compact keys per entry.
	var raw struct {
		Collections []map[string]any `json:"collections"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, entry := range raw.Collections {
		if len(entry) != 5 {
			t.Fatalf("summary keys=%v want exactly 5", entry)
		}
		for _, key := range []string{"id", "name", "version", "member_count", "visible_post_count"} {
			if _, ok := entry[key]; !ok {
				t.Fatalf("summary missing key %q: %v", key, entry)
			}
		}
	}
	// Legacy list route is unavailable without Collections, summaries still work.
	if resp := requestPath(t, router, "/api/collections"); resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("legacy list without capability: status=%d want 503", resp.Code)
	}
}

func TestCollectionSummariesEmptyList(t *testing.T) {
	reader := &fakeCollectionSummaryReader{summaries: nil}
	router := NewRouterWithDependencies("", Dependencies{CollectionSummaries: reader})
	response := requestPath(t, router, "/api/collections/summaries")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"collections":[]}` {
		t.Fatalf("empty summaries: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCollectionSummariesUnavailableCapability(t *testing.T) {
	backend := &oldStyleCollections{}
	if _, ok := any(backend).(posts.CollectionSummaryReader); ok {
		t.Fatal("old-style backend unexpectedly implements CollectionSummaryReader")
	}
	for _, router := range []http.Handler{
		NewRouter(backend),
		NewRouterWithDependencies("", Dependencies{Collections: backend}),
		NewRouterWithDependencies("", Dependencies{}),
	} {
		if resp := requestPath(t, router, "/api/collections/summaries"); resp.Code != http.StatusServiceUnavailable {
			t.Fatalf("summaries without capability: status=%d want 503 body=%s", resp.Code, resp.Body.String())
		}
	}
	// Old routes still work on the old backend.
	router := NewRouter(backend)
	if resp := requestPath(t, router, "/api/collections"); resp.Code != http.StatusOK {
		t.Fatalf("legacy list: status=%d body=%s", resp.Code, resp.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Favorites"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("legacy create: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCollectionSummariesLegacyDiscovery(t *testing.T) {
	backend := &legacySummaryBackend{
		oldStyleCollections: &oldStyleCollections{},
		summaries:           []posts.CollectionSummary{{ID: "1", Name: "Favorites", Version: 2, MemberCount: 3, VisiblePostCount: 2}},
	}
	var _ posts.CollectionSummaryReader = backend
	router := NewRouter(backend)
	response := requestPath(t, router, "/api/collections/summaries")
	if response.Code != http.StatusOK {
		t.Fatalf("legacy summaries: status=%d body=%s", response.Code, response.Body.String())
	}
	var got struct {
		Collections []posts.CollectionSummary `json:"collections"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Collections) != 1 || got.Collections[0].MemberCount != 3 {
		t.Fatalf("legacy summaries body: %#v", got)
	}
	// Independent explicit wiring: summary reader alone, no mutator needed.
	reader := &fakeCollectionSummaryReader{summaries: backend.summaries}
	explicit := NewRouterWithDependencies("", Dependencies{CollectionSummaries: reader})
	if resp := requestPath(t, explicit, "/api/collections/summaries"); resp.Code != http.StatusOK {
		t.Fatalf("explicit summaries: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCollectionSummariesPublicReadAuth(t *testing.T) {
	reader := &fakeCollectionSummaryReader{summaries: []posts.CollectionSummary{{ID: "1", Name: "A", MemberCount: 1, VisiblePostCount: 1}}}
	backend := &oldStyleCollections{}
	router := NewRouterWithDependencies("secret", Dependencies{Collections: backend, CollectionSummaries: reader})
	// Public read: no token, and even a wrong token, may read summaries.
	for _, auth := range []string{"", "Bearer " + "wrong"} {
		request := httptest.NewRequest(http.MethodGet, "/api/collections/summaries", nil)
		if auth != "" {
			request.Header.Set("Authorization", auth)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("public summaries auth=%q: status=%d body=%s", auth, response.Code, response.Body.String())
		}
	}
	// Writes still require the token.
	request := httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Nope"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated write: status=%d want 401", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Ok"}`))
	request.Header.Set("Authorization", "Bearer "+"secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("authenticated write: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCollectionSummariesErrorMapping(t *testing.T) {
	reader := &fakeCollectionSummaryReader{err: posts.ErrUnavailable}
	router := NewRouterWithDependencies("", Dependencies{CollectionSummaries: reader})
	if resp := requestPath(t, router, "/api/collections/summaries"); resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable reader: status=%d want 503", resp.Code)
	}
}
