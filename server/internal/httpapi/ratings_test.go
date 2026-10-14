package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeRatingMutator struct {
	request posts.RatingRequest
	result  posts.RatingResult
	err     error
}

func (f *fakeRatingMutator) UpdateRatings(_ context.Context, request posts.RatingRequest) (posts.RatingResult, error) {
	f.request = request
	return f.result, f.err
}

func ratingRequest(handler http.Handler, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/posts/ratings", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestEditRatingsAcceptsVersionZeroAndReturnsAtomicResult(t *testing.T) {
	fake := &fakeRatingMutator{result: posts.RatingResult{Posts: []posts.RatingOutcome{{ID: "10", Version: 1, Rating: posts.RatingSafe, Changed: true}}}}
	response := ratingRequest(EditRatings(fake), `{"posts":[{"id":"10","version":0}],"rating":"safe"}`)
	if response.Code != http.StatusOK || fake.request.Posts[0].Version == nil || *fake.request.Posts[0].Version != 0 || !strings.Contains(response.Body.String(), `"rating":"safe"`) {
		t.Fatalf("status=%d request=%#v body=%s", response.Code, fake.request, response.Body.String())
	}
}

func TestEditRatingsRejectsMalformedRequests(t *testing.T) {
	handler := EditRatings(&fakeRatingMutator{})
	for _, body := range []string{
		`{"posts":[{"id":"10"}],"rating":"safe"}`,
		`{"posts":[{"id":"10","version":null}],"rating":"safe"}`,
		`{"posts":[{"id":"10","version":0}],"rating":"SAFE"}`,
		`{"posts":[{"id":"10","version":0}],"rating":"safe","extra":true}`,
		`{"posts":[{"id":"10","version":0}],"rating":"unrated","posts2":[]}`,
	} {
		if got := ratingRequest(handler, body).Code; got != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400", body, got)
		}
	}
}

func TestEditRatingsMapsConflictMissingAndUnavailable(t *testing.T) {
	fake := &fakeRatingMutator{err: posts.ErrRatingConflict}
	handler := EditRatings(fake)
	body := `{"posts":[{"id":"10","version":0}],"rating":"safe"}`
	if got := ratingRequest(handler, body).Code; got != http.StatusConflict {
		t.Fatalf("conflict status=%d", got)
	}
	fake.err = posts.ErrNotFound
	if got := ratingRequest(handler, body).Code; got != http.StatusNotFound {
		t.Fatalf("missing status=%d", got)
	}
	fake.err = posts.ErrUnavailable
	if got := ratingRequest(handler, body).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", got)
	}
	fake.err = errors.New("database failure")
	if got := ratingRequest(handler, body).Code; got != http.StatusInternalServerError {
		t.Fatalf("internal status=%d", got)
	}
	if got := ratingRequest(EditRatings(nil), body).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("nil mutator status=%d", got)
	}
}
