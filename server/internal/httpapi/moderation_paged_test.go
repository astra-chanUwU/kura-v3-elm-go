package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

type fakePager struct {
	fakeModerator
	gotCursor *string
	page      posts.ModerationQueuePage
	pageErr   error
}

func (f *fakePager) ListModerationQueuePage(_ context.Context, state string, cursor *string, limit int) (posts.ModerationQueuePage, error) {
	f.gotState, f.gotLimit, f.gotCursor = state, limit, cursor
	return f.page, f.pageErr
}

type fakePreview struct {
	preview posts.ModerationPreview
	err     error
	calls   int
	gotID   string
}

func (f *fakePreview) OpenModerationPreview(_ context.Context, id string) (posts.ModerationPreview, error) {
	f.calls++
	f.gotID = id
	return f.preview, f.err
}

func pagedRouter(token string, reader posts.ModerationReader, preview posts.ModerationPreviewReader) http.Handler {
	// Mirror production middleware order for the two new routes only.
	deps := Dependencies{}
	_ = deps
	r := chi.NewRouter()
	r.Use(requireWriteCapability(token))
	r.Get("/api/posts/moderation", listModerationQueuePaged(reader))
	r.Get("/api/posts/{id}/moderation/preview", moderationPreview(preview))
	return r
}

func get(t *testing.T, router http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPagedQueueValidatesCursorAndState(t *testing.T) {
	cursor, _ := posts.EncodeModerationQueueCursor("hidden", 99)
	fake := &fakePager{page: posts.ModerationQueuePage{Posts: []posts.ModerationQueueItem{}}}
	router := pagedRouter("secret", fake, nil)
	if rec := get(t, router, "/api/posts/moderation?state=hidden&cursor="+cursor, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon paged queue = %d", rec.Code)
	}
	if rec := get(t, router, "/api/posts/moderation?state=hidden&cursor="+cursor, "secret"); rec.Code != http.StatusOK || fake.gotCursor == nil || *fake.gotCursor != cursor {
		t.Fatalf("paged queue = %d cursor=%v body=%s", rec.Code, fake.gotCursor, rec.Body.String())
	}
	other, _ := posts.EncodeModerationQueueCursor("published", 99)
	if rec := get(t, router, "/api/posts/moderation?state=hidden&cursor="+other, "secret"); rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-state cursor = %d", rec.Code)
	}
	if rec := get(t, router, "/api/posts/moderation?state=hidden&cursor=garbage", "secret"); rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage cursor = %d", rec.Code)
	}
}

func TestPreviewAuthorizesBeforeLookup(t *testing.T) {
	fake := &fakePreview{preview: posts.ModerationPreview{Body: io.NopCloser(strings.NewReader("jpeg-bytes")), MediaType: "image/jpeg", Length: 10}}
	router := pagedRouter("secret", &fakePager{}, fake)
	if rec := get(t, router, "/api/posts/2004/moderation/preview", ""); rec.Code != http.StatusUnauthorized || fake.calls != 0 {
		t.Fatalf("anon preview = %d calls=%d (lookup must not run)", rec.Code, fake.calls)
	}
	rec := get(t, router, "/api/posts/2004/moderation/preview", "secret")
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg-bytes" || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("preview = %d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
	}
	if fake.gotID != "2004" {
		t.Fatalf("preview id = %q", fake.gotID)
	}
}

func TestPreviewErrorMapping(t *testing.T) {
	for err, want := range map[error]int{
		posts.ErrNotFound: http.StatusNotFound, posts.ErrModerationPreviewUnavailable: http.StatusConflict,
		posts.ErrUnavailable: http.StatusServiceUnavailable, posts.ErrInvalidModeration: http.StatusBadRequest,
	} {
		fake := &fakePreview{err: err}
		router := pagedRouter("secret", &fakePager{}, fake)
		if rec := get(t, router, "/api/posts/9/moderation/preview", "secret"); rec.Code != want {
			t.Errorf("err %v -> %d want %d", err, rec.Code, want)
		}
	}
	// Non-JPEG and oversized descriptors fail closed without serving bytes.
	for _, p := range []posts.ModerationPreview{
		{Body: io.NopCloser(strings.NewReader("x")), MediaType: "image/png", Length: 1},
		{Body: io.NopCloser(strings.NewReader("x")), MediaType: "image/jpeg", Length: maxModerationPreviewBytes + 1},
	} {
		router := pagedRouter("secret", &fakePager{}, &fakePreview{preview: p})
		if rec := get(t, router, "/api/posts/9/moderation/preview", "secret"); rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), "\"x\"") && rec.Body.String() == "x" {
			t.Errorf("unsafe descriptor -> %d body=%q", rec.Code, rec.Body.String())
		}
	}
	// Oversized body streams are capped even when Length lies.
	big := &fakePreview{preview: posts.ModerationPreview{Body: io.NopCloser(strings.NewReader(strings.Repeat("a", maxModerationPreviewBytes+2))), MediaType: "image/jpeg", Length: 1}}
	router := pagedRouter("secret", &fakePager{}, big)
	if rec := get(t, router, "/api/posts/9/moderation/preview", "secret"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("oversized body -> %d", rec.Code)
	}
}
