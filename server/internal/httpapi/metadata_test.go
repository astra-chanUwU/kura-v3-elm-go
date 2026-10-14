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

type fakeMetadataMutator struct {
	request posts.MetadataRequest
	result  posts.MetadataResponse
	err     error
}

func (f *fakeMetadataMutator) UpdateMetadata(_ context.Context, request posts.MetadataRequest) (posts.MetadataResponse, error) {
	f.request = request
	return f.result, f.err
}

func metadataRequest(handler http.Handler, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/posts/metadata", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestMetadataHandlerPreservesOmittedAndEmptyFields(t *testing.T) {
	fake := &fakeMetadataMutator{result: posts.MetadataResponse{Posts: []posts.MetadataResult{{ID: "2004", Version: 1, Source: "", Artist: "artist", Changed: true}}}}
	handler := EditMetadata(fake)
	response := metadataRequest(handler, `{"posts":[{"id":"2004","version":0}],"source":""}`)
	if response.Code != http.StatusOK || fake.request.Source == nil || *fake.request.Source != "" || fake.request.Artist != nil {
		t.Fatalf("empty/omitted fields collapsed: status=%d request=%#v body=%s", response.Code, fake.request, response.Body.String())
	}
	for _, body := range []string{
		`{"posts":[{"id":"2004"}],"source":"value"}`,
		`{"posts":[{"id":"2004","version":null}],"source":"value"}`,
		`{"posts":[{"id":"2004","version":0}],"source":"value","extra":true}`,
		`{"posts":[{"id":"2004","version":0}]}`,
	} {
		if got := metadataRequest(handler, body).Code; got != http.StatusBadRequest {
			t.Errorf("body %s: status=%d want 400", body, got)
		}
	}
}

func TestMetadataHandlerMapsConflictAndMissing(t *testing.T) {
	fake := &fakeMetadataMutator{err: posts.ErrMetadataConflict}
	handler := EditMetadata(fake)
	body := `{"posts":[{"id":"2004","version":0}],"artist":"artist"}`
	if got := metadataRequest(handler, body).Code; got != http.StatusConflict {
		t.Fatalf("conflict status=%d want 409", got)
	}
	fake.err = posts.ErrNotFound
	if got := metadataRequest(handler, body).Code; got != http.StatusNotFound {
		t.Fatalf("missing status=%d want 404", got)
	}
	fake.err = errors.New("database failed")
	if got := metadataRequest(handler, body).Code; got != http.StatusInternalServerError {
		t.Fatalf("unexpected status=%d want 500", got)
	}
}

func TestMetadataHandlerUnavailable(t *testing.T) {
	if got := metadataRequest(EditMetadata(nil), `{"posts":[{"id":"2004","version":0}],"source":"x"}`).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d want 503", got)
	}
}
