package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type metadataReadFake struct {
	calls int
	ids   []string
	err   error
}

func (f *metadataReadFake) ReadMetadata(_ context.Context, request posts.MetadataReadRequest) (posts.MetadataReadResponse, error) {
	f.calls++
	f.ids = request.PostIDs
	return posts.MetadataReadResponse{Posts: []posts.MetadataSnapshot{{ID: "2", Version: 0}}}, f.err
}

func TestMetadataReadRouteIsProtectedAndBounded(t *testing.T) {
	fake := &metadataReadFake{}
	router := NewRouterWithDependencies("secret", Dependencies{MetadataRead: fake})
	request := func(token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/posts/metadata/read", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if got := request("", `{"post_ids":["2"]}`).Code; got != 401 || fake.calls != 0 {
		t.Fatalf("unauthorized status=%d calls=%d", got, fake.calls)
	}
	for _, body := range []string{`{}`, `{"post_ids":null}`, `{"post_ids":[" 2"]}`, `{"post_ids":["2","2"]}`, `{"post_ids":["2"],"extra":true}`} {
		if got := request("secret", body).Code; got != 400 || fake.calls != 0 {
			t.Fatalf("malformed body %s: status=%d calls=%d", body, got, fake.calls)
		}
	}
	got := request("secret", `{"post_ids":["2"]}`)
	if got.Code != 200 || !reflect.DeepEqual(fake.ids, []string{"2"}) || !strings.Contains(got.Body.String(), `"version":0`) {
		t.Fatalf("read status=%d body=%s", got.Code, got.Body.String())
	}
	fake.err = posts.ErrNotFound
	if got := request("secret", `{"post_ids":["2"]}`).Code; got != 404 {
		t.Fatalf("inaccessible status=%d", got)
	}
}
