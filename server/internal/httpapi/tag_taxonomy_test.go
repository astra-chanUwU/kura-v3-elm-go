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

type fakeTaxonomy struct {
	page    posts.TagManagePage
	preview posts.TagManagePreview
	result  posts.TagManageResult
	err     error
	actor   string
}

func (f *fakeTaxonomy) ListManageTags(context.Context, string, int, *posts.TagManageCursor) (posts.TagManagePage, error) {
	return f.page, f.err
}
func (f *fakeTaxonomy) PreviewTagOperation(context.Context, posts.TagManageRequest) (posts.TagManagePreview, error) {
	return f.preview, f.err
}
func (f *fakeTaxonomy) ApplyTagOperation(_ context.Context, actor string, _ posts.TagManageRequest) (posts.TagManageResult, error) {
	f.actor = actor
	return f.result, f.err
}

func doTax(t *testing.T, router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestTagManageGetChecksActorBeforeNilBackend(t *testing.T) {
	router := NewRouterWithDependencies("secret", Dependencies{})
	if rec := doTax(t, router, "GET", "/api/tags/manage", "", ""); rec.Code != 401 {
		t.Fatalf("unauth before nil: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTax(t, router, "GET", "/api/tags/manage", "secret", ""); rec.Code != 503 {
		t.Fatalf("auth nil backend: %d", rec.Code)
	}
}

func TestTagManageHandlersAndStaleCode(t *testing.T) {
	fake := &fakeTaxonomy{page: posts.TagManagePage{Version: 3, Tags: []posts.TagManageEntry{{Tag: "cat", PostCount: 2, VisiblePostCount: 1}}}, preview: posts.TagManagePreview{Version: 3, Mode: "rename", SourceTag: "a", TargetTag: "b", AffectedPostCount: 1}, result: posts.TagManageResult{ID: "9", Version: 4, Mode: "rename", Changed: true}}
	router := NewRouterWithDependencies("secret", Dependencies{TagTaxonomy: fake})
	if rec := doTax(t, router, "GET", "/api/tags/manage?prefix=c&limit=10", "secret", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"post_count":2`) {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTax(t, router, "GET", "/api/tags/manage?limit=0", "secret", ""); rec.Code != 400 {
		t.Fatalf("limit %d", rec.Code)
	}
	if rec := doTax(t, router, "GET", "/api/tags/manage?cursor=bad", "secret", ""); rec.Code != 400 {
		t.Fatalf("cursor %d", rec.Code)
	}
	for _, query := range []string{"prefix=a&prefix=b", "cursor=a&cursor=b", "limit=1&limit=2", "unknown=x"} {
		if rec := doTax(t, router, "GET", "/api/tags/manage?"+query, "secret", ""); rec.Code != 400 {
			t.Fatalf("query %q: %d", query, rec.Code)
		}
	}
	if rec := doTax(t, router, "POST", "/api/tags/manage/preview", "secret", `{"mode":"rename","source_tag":"a","target_tag":"b"}`); rec.Code != 200 {
		t.Fatalf("preview %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTax(t, router, "POST", "/api/tags/manage/apply", "secret", `{"mode":"rename","source_tag":"a","target_tag":"b"}`); rec.Code != 400 {
		t.Fatalf("missing expected %d %s", rec.Code, rec.Body.String())
	}
	rec := doTax(t, router, "POST", "/api/tags/manage/apply", "secret", `{"mode":"rename","source_tag":"a","target_tag":"b","expected_version":3}`)
	if rec.Code != 200 || fake.actor != "system" {
		t.Fatalf("apply %d actor=%q %s", rec.Code, fake.actor, rec.Body.String())
	}
	var decoded posts.TagManageResult
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil || decoded.ID != "9" {
		t.Fatalf("decoded %#v err=%v", decoded, err)
	}
	fake.err = posts.ErrTagTaxonomyChanged
	if rec := doTax(t, router, "GET", "/api/tags/manage", "secret", ""); rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"tag_taxonomy_changed"`) {
		t.Fatalf("stale %d %s", rec.Code, rec.Body.String())
	}
	fake.err = posts.ErrTagOperationTooLarge
	if rec := doTax(t, router, "POST", "/api/tags/manage/preview", "secret", `{"mode":"rename","source_tag":"a","target_tag":"b"}`); rec.Code != 400 {
		t.Fatalf("too large %d", rec.Code)
	}
	fake.err = posts.ErrTagRetired
	if rec := doTax(t, router, "POST", "/api/tags/manage/preview", "secret", `{"mode":"rename","source_tag":"a","target_tag":"b"}`); rec.Code != 409 {
		t.Fatalf("retired %d", rec.Code)
	}
}
