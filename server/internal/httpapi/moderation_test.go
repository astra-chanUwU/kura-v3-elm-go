package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeModerator struct {
	actor         string
	action        posts.ModerationResponse
	actionErr     error
	gotActor      string
	gotRequest    posts.ModerationRequest
	history       []posts.ModerationAction
	historyErr    error
	gotHistoryID  string
	queue         posts.ModerationQueuePage
	queueErr      error
	gotState      string
	gotLimit      int
	visibility    map[string]posts.MediaVisibility
	visibilityErr error
}

func (f *fakeModerator) SearchPosts(context.Context, string, *posts.Cursor, int) (posts.SearchPage, error) {
	return posts.SearchPage{}, nil
}

func (f *fakeModerator) ModeratePosts(_ context.Context, actor string, request posts.ModerationRequest) (posts.ModerationResponse, error) {
	f.gotActor, f.gotRequest = actor, request
	return f.action, f.actionErr
}

func (f *fakeModerator) ListModerationActions(_ context.Context, postID string) ([]posts.ModerationAction, error) {
	f.gotHistoryID = postID
	return f.history, f.historyErr
}

func (f *fakeModerator) ListModerationQueue(_ context.Context, state string, limit int) (posts.ModerationQueuePage, error) {
	f.gotState, f.gotLimit = state, limit
	return f.queue, f.queueErr
}

func (f *fakeModerator) LookupMediaVisibility(_ context.Context, mediaPath string) (posts.MediaVisibility, error) {
	if f.visibilityErr != nil {
		return posts.MediaVisibility{}, f.visibilityErr
	}
	return f.visibility[mediaPath], nil
}

func moderateRequest(t *testing.T, router http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/posts/moderation", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestModeratePostsRequiresCapability(t *testing.T) {
	fake := &fakeModerator{action: posts.ModerationResponse{Posts: []posts.ModerationResult{{ID: "2004", Action: "hide", PreviousState: "published", NewState: "hidden", Changed: true}}}}
	router := NewRouterWithToken("secret", fake)

	if response := moderateRequest(t, router, "", `{"posts":[{"id":"2004"}],"action":"hide"}`); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", response.Code, response.Body.String())
	}
	response := moderateRequest(t, router, "secret", `{"posts":[{"id":"2004"}],"action":"hide","reason":"spam"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized moderation status=%d body=%s", response.Code, response.Body.String())
	}
	if fake.gotActor != "system" || fake.gotRequest.Action != "hide" || len(fake.gotRequest.Posts) != 1 {
		t.Fatalf("moderation not attributed: actor=%q request=%#v", fake.gotActor, fake.gotRequest)
	}
	var decoded posts.ModerationResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Posts) != 1 || !decoded.Posts[0].Changed || decoded.Posts[0].NewState != posts.ModerationHidden {
		t.Fatalf("unexpected moderation body: %#v", decoded)
	}
}

func TestModeratePostsValidationAndErrors(t *testing.T) {
	fake := &fakeModerator{}
	router := NewRouter(fake)
	for _, body := range []string{
		`{"posts":[],"action":"hide"}`,
		`{"posts":[{"id":"2004"}],"action":"ban"}`,
		`{"posts":[{"id":"bad"}],"action":"hide"}`,
	} {
		if response := moderateRequest(t, router, "", body); response.Code != http.StatusBadRequest {
			t.Errorf("body %s: got status %d", body, response.Code)
		}
	}
	fake.actionErr = posts.ErrNotFound
	if response := moderateRequest(t, router, "", `{"posts":[{"id":"2004"}],"action":"hide"}`); response.Code != http.StatusNotFound {
		t.Fatalf("missing post status=%d", response.Code)
	}
	fake.actionErr = posts.ErrUnavailable
	if response := moderateRequest(t, router, "", `{"posts":[{"id":"2004"}],"action":"hide"}`); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", response.Code)
	}
}

func TestModerationQueueRequiresCapabilityAndState(t *testing.T) {
	fake := &fakeModerator{queue: posts.ModerationQueuePage{Posts: []posts.ModerationQueueItem{{ModerationState: posts.ModerationHidden}}}}
	router := NewRouterWithToken("secret", fake)

	request := httptest.NewRequest(http.MethodGet, "/api/posts/moderation?state=hidden", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous queue status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/posts/moderation?state=hidden&limit=10", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fake.gotState != "hidden" || fake.gotLimit != 10 {
		t.Fatalf("queue: status=%d state=%q limit=%d body=%s", response.Code, fake.gotState, fake.gotLimit, response.Body.String())
	}
	// The static moderation path must win over /api/posts/{id}.
	var decoded posts.ModerationQueuePage
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Posts) != 1 || decoded.Posts[0].ModerationState != posts.ModerationHidden {
		t.Fatalf("queue routed to post detail: %#v", decoded)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/posts/moderation", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing state status=%d", response.Code)
	}
	fake.queueErr = posts.ErrInvalidModeration
	request = httptest.NewRequest(http.MethodGet, "/api/posts/moderation?state=banned", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown state status=%d", response.Code)
	}
}

func TestPostModerationActionsRoute(t *testing.T) {
	fake := &fakeModerator{history: []posts.ModerationAction{{ID: "1", PostID: "2004", Action: "hide", PreviousState: "published", NewState: "hidden", Actor: "system"}}}
	router := NewRouter(fake)

	anonymous := NewRouterWithToken("secret", fake)
	request := httptest.NewRequest(http.MethodGet, "/api/posts/2004/moderation", nil)
	response := httptest.NewRecorder()
	anonymous.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous audit status=%d", response.Code)
	}

	response = requestPath(t, router, "/api/posts/2004/moderation")
	if response.Code != http.StatusOK || fake.gotHistoryID != "2004" {
		t.Fatalf("audit: status=%d id=%q body=%s", response.Code, fake.gotHistoryID, response.Body.String())
	}
	var decoded struct {
		Actions []posts.ModerationAction `json:"actions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Actions) != 1 || decoded.Actions[0].Actor != "system" {
		t.Fatalf("unexpected audit body: %#v", decoded)
	}
	fake.historyErr = posts.ErrNotFound
	if response := requestPath(t, router, "/api/posts/2004/moderation"); response.Code != http.StatusNotFound {
		t.Fatalf("missing audit status=%d", response.Code)
	}
	if errors.Is(posts.ErrInvalidModeration, posts.ErrNotFound) {
		t.Fatal("moderation errors must stay distinct")
	}
}

func TestGatedMediaDeniesNonPublishedPosts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"published.jpg", "hidden.jpg", "loose.jpg"} {
		if err := os.WriteFile(filepath.Join(root, "uploads", name), []byte("bytes-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeModerator{visibility: map[string]posts.MediaVisibility{
		"/media/uploads/published.jpg": {Referenced: true, Visible: true},
		"/media/uploads/hidden.jpg":    {Referenced: true, Visible: false},
	}}
	router := NewRouter(fake)

	for path, want := range map[string]int{
		"/media/uploads/published.jpg": http.StatusOK,
		"/media/uploads/hidden.jpg":    http.StatusNotFound,
		"/media/uploads/loose.jpg":     http.StatusOK,
		"/media/uploads/missing.jpg":   http.StatusNotFound,
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Errorf("%s: got status %d, want %d", path, response.Code, want)
		}
	}
	if body := requestPath(t, router, "/media/uploads/hidden.jpg").Body.String(); body == "bytes-hidden.jpg" {
		t.Error("hidden media bytes leaked through direct URL")
	}
}

func TestGatedMediaWithoutCheckerPreservesPublicBehavior(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "plain.jpg"), []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(&fakeSearcher{})
	if response := requestPath(t, router, "/media/uploads/plain.jpg"); response.Code != http.StatusOK {
		t.Fatalf("checkerless media status=%d", response.Code)
	}
}
