package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearch_SuccessWithNextCursor(t *testing.T) {
	var gotQ, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		gotRaw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"post-123","preview_url":"/media/p.jpg","original_url":"/media/o.jpg","media_type":"image/jpeg","width":640,"height":480}],"next_cursor":"tok123"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx := context.Background()
	res, err := c.Search(ctx, "cat feline")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if gotQ != "cat feline" {
		t.Fatalf("query mismatch: got %q want %q", gotQ, "cat feline")
	}
	if !strings.Contains(gotRaw, "q=cat+feline") {
		t.Fatalf("raw query not encoded: %q", gotRaw)
	}
	if len(res.Posts) != 1 || res.Posts[0].ID != "post-123" {
		t.Fatalf("posts mismatch: %+v", res.Posts)
	}
	if res.NextCursor == nil || *res.NextCursor != "tok123" {
		t.Fatalf("next_cursor mismatch: %v", res.NextCursor)
	}
}

func TestSearch_EmptyBrowse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "" {
			t.Fatalf("expected empty q, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"post-newest","preview_url":"/media/newest.jpg","original_url":"/media/newest-original.jpg","media_type":"image/jpeg","width":640,"height":480}],"next_cursor":"browse-next"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	res, err := c.Search(context.Background(), "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(res.Posts) != 1 || res.Posts[0].ID != "post-newest" {
		t.Fatalf("expected newest browse post, got %#v", res.Posts)
	}
	if res.NextCursor == nil || *res.NextCursor != "browse-next" {
		t.Fatalf("expected browse cursor, got %v", res.NextCursor)
	}
}

func TestSearch_URLEncoding(t *testing.T) {
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRaw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.Search(context.Background(), "a&b=c d")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	// net/url Encode encodes space as + and & as %26
	if gotRaw != "q=a%26b%3Dc+d" {
		t.Fatalf("encoding mismatch: got %q want %q", gotRaw, "q=a%26b%3Dc+d")
	}
}

func TestSearch_APIErrorReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"search query is too long (maximum 256 characters)"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.Search(context.Background(), strings.Repeat("x", 300))
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 400 {
		t.Fatalf("status mismatch: got %d", apiErr.Status)
	}
	if !strings.Contains(apiErr.Message, "too long") {
		t.Fatalf("message mismatch: %q", apiErr.Message)
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("error string not readable: %q", err.Error())
	}
}

func TestSearch_ServiceUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"post search unavailable"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.Search(context.Background(), "cat")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "post search unavailable") {
		t.Fatalf("expected unavailable message, got %q", err.Error())
	}
}

func TestSearch_RequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTPClient.Timeout = 50 * time.Millisecond
	_, err := c.Search(context.Background(), "cat")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("expected request failed, got %q", err.Error())
	}
}

func TestSearch_InvalidAPIURL(t *testing.T) {
	c := New("://bad")
	_, err := c.Search(context.Background(), "cat")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestSearch_NilPostsNormalized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":null}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	res, err := c.Search(context.Background(), "cat")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if res.Posts == nil {
		t.Fatal("expected non-nil posts slice")
	}
	if len(res.Posts) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(res.Posts))
	}
}

func TestSearchPage_PaginationParams(t *testing.T) {
	var gotQ, gotCursor, gotLimit, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("q")
		gotCursor = r.URL.Query().Get("cursor")
		gotLimit = r.URL.Query().Get("limit")
		gotRaw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[],"next_cursor":null}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.SearchPage(context.Background(), "cat", "tok123", 20)
	if err != nil {
		t.Fatalf("SearchPage error: %v", err)
	}
	if gotQ != "cat" || gotCursor != "tok123" || gotLimit != "20" {
		t.Fatalf("pagination params mismatch q=%q cursor=%q limit=%q raw=%q", gotQ, gotCursor, gotLimit, gotRaw)
	}
}

func TestSearchPage_LimitBoundaries(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	// valid boundaries 1 and 60 should succeed
	for _, lim := range []int{1, 60} {
		hit = false
		if _, err := c.SearchPage(context.Background(), "cat", "", lim); err != nil {
			t.Fatalf("limit %d should succeed: %v", lim, err)
		}
		if !hit {
			t.Fatalf("limit %d did not hit server", lim)
		}
	}
	// invalid limits should error without request
	for _, lim := range []int{0, -1, 61, 100} {
		if lim == 0 {
			// 0 means omit, should succeed
			continue
		}
		hit = false
		if _, err := c.SearchPage(context.Background(), "cat", "", lim); err == nil {
			t.Fatalf("limit %d should fail", lim)
		}
		if hit {
			t.Fatalf("invalid limit %d should not hit server", lim)
		}
		if _, err := c.SearchPage(context.Background(), "cat", "tok", lim); err == nil {
			t.Fatalf("limit %d with cursor should fail", lim)
		}
	}
}

func TestSearchPage_CursorOpaqueEncoding(t *testing.T) {
	var gotCursor string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursor = r.URL.Query().Get("cursor")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	opaque := "a/b+c=123=="
	if _, err := c.SearchPage(context.Background(), "cat", opaque, 10); err != nil {
		t.Fatalf("SearchPage error: %v", err)
	}
	if gotCursor != opaque {
		t.Fatalf("cursor mismatch got %q want %q", gotCursor, opaque)
	}
}

func TestSearchPage_OmitEmptyCursorAndLimit(t *testing.T) {
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRaw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.SearchPage(context.Background(), "cat", "", 0); err != nil {
		t.Fatalf("SearchPage error: %v", err)
	}
	if strings.Contains(gotRaw, "cursor=") || strings.Contains(gotRaw, "limit=") {
		t.Fatalf("expected no cursor/limit params, got %q", gotRaw)
	}
}

func TestSearch_IsWrapper(t *testing.T) {
	var gotCursor, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursor = r.URL.Query().Get("cursor")
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.Search(context.Background(), "hello"); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if gotCursor != "" || gotLimit != "" {
		t.Fatalf("Search wrapper should not send cursor/limit, got cursor=%q limit=%q", gotCursor, gotLimit)
	}
}

func TestListCollections_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collections":[{"id":"7","name":"Favorites","post_ids":["11","10"]}]}`))
	}))
	defer srv.Close()

	result, err := New(srv.URL).ListCollections(context.Background())
	if err != nil {
		t.Fatalf("ListCollections error: %v", err)
	}
	if gotPath != "/api/collections" {
		t.Fatalf("path mismatch: got %q", gotPath)
	}
	if len(result.Collections) != 1 || result.Collections[0].Name != "Favorites" || len(result.Collections[0].PostIDs) != 2 {
		t.Fatalf("unexpected collections: %#v", result.Collections)
	}
}

func TestCollectionPosts_SuccessAndAPIError(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/api/collections/7/posts" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"posts":[{"id":"11","media_type":"image/jpeg","width":10,"height":20}],"next_cursor":"next"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"collection or post not found"}`))
	}))
	defer srv.Close()

	result, err := New(srv.URL).CollectionPosts(context.Background(), "7")
	if err != nil {
		t.Fatalf("CollectionPosts error: %v", err)
	}
	if gotPath != "/api/collections/7/posts" || len(result.Posts) != 1 || result.NextCursor == nil || *result.NextCursor != "next" {
		t.Fatalf("unexpected request/result path=%q result=%#v", gotPath, result)
	}
	_, err = New(srv.URL).ListCollectionPosts(context.Background(), "8")
	if err == nil || !strings.Contains(err.Error(), "collection or post not found") {
		t.Fatalf("expected readable collection API error, got %v", err)
	}
}

func TestCollectionPosts_EmptyID(t *testing.T) {
	if _, err := New("http://example.test").CollectionPosts(context.Background(), " "); err == nil {
		t.Fatal("expected empty collection id error")
	}
}

func TestGetPostDetail_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"2004","preview_url":"/preview","original_url":"/original","media_type":"image/jpeg","width":679,"height":437,"source":"demo/kson.jpeg","artist":"Kura Demo","hash":"sha256:test","file_size":165554,"created_at":"2026-01-02 00:00:04+00","tags":["demo"],"tag_version":2,"favorite":true,"score":3,"reaction_version":1,"history":[{"version":2,"kind":"tag_edit","added_tags":["night"],"removed_tags":["demo"],"target_tags":["kson","night"],"created_at":"2026-01-02 00:00:06+00"}],"reaction_history":[{"version":1,"favorite":true,"score":3,"created_at":"2026-01-03 00:00:00+00"}]}`))
	}))
	defer srv.Close()

	detail, err := New(srv.URL).GetPostDetail(context.Background(), "2004")
	if err != nil {
		t.Fatalf("GetPostDetail error: %v", err)
	}
	if gotPath != "/api/posts/2004" || detail.ID != "2004" || detail.Source != "demo/kson.jpeg" || len(detail.History) != 1 || len(detail.ReactionHistory) != 1 {
		t.Fatalf("unexpected detail path=%q detail=%#v", gotPath, detail)
	}
}

func TestGetPostDetail_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"post not found"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).GetPostDetail(context.Background(), "404")
	if err == nil || !strings.Contains(err.Error(), "post not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestGetPostDetail_EmptyID(t *testing.T) {
	if _, err := New("http://example.test").GetPostDetail(context.Background(), " "); err == nil {
		t.Fatal("expected empty id error")
	}
}

func TestEditTags_SuccessAndConflict(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"2004","version":3,"tags":["kson"],"changed":true}]}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL).EditTags(context.Background(), TagEditRequest{
		Posts: []TagTarget{{ID: "2004", Version: 2}},
		Add:   []string{"night"},
	})
	if err != nil {
		t.Fatalf("EditTags error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/posts/tags" || !strings.Contains(gotBody, "night") {
		t.Fatalf("unexpected edit request method=%q path=%q body=%q", gotMethod, gotPath, gotBody)
	}
	if len(res.Posts) != 1 || res.Posts[0].Version != 3 {
		t.Fatalf("unexpected edit result: %#v", res)
	}
	// Conflict
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"post tag version is stale"}`))
	}))
	defer srv2.Close()
	_, err = New(srv2.URL).EditTags(context.Background(), TagEditRequest{Posts: []TagTarget{{ID: "2004", Version: 0}}, Add: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestRevertTags_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"2004","version":4,"tags":["demo"],"changed":true}]}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL).RevertTags(context.Background(), TagRevertRequest{
		Posts:         []TagTarget{{ID: "2004", Version: 3}},
		TargetVersion: 1,
	})
	if err != nil {
		t.Fatalf("RevertTags error: %v", err)
	}
	if gotPath != "/api/posts/tags/revert" || len(res.Posts) != 1 || res.Posts[0].Version != 4 {
		t.Fatalf("unexpected revert path=%q result=%#v", gotPath, res)
	}
}

func TestEditReactions_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[{"id":"2004","version":2,"favorite":true,"score":5,"changed":true}]}`))
	}))
	defer srv.Close()
	fav := true
	score := 5
	res, err := New(srv.URL).EditReactions(context.Background(), ReactionRequest{
		Posts:    []ReactionTarget{{ID: "2004", Version: 1}},
		Favorite: &fav,
		Score:    &score,
	})
	if err != nil {
		t.Fatalf("EditReactions error: %v", err)
	}
	if gotPath != "/api/posts/reactions" || len(res.Posts) != 1 || !res.Posts[0].Favorite || res.Posts[0].Score != 5 {
		t.Fatalf("unexpected reaction path=%q result=%#v", gotPath, res)
	}
}

func TestEditReactions_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"post not found"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).EditReactions(context.Background(), ReactionRequest{Posts: []ReactionTarget{{ID: "404", Version: 0}}, Favorite: func() *bool { b := true; return &b }()})
	if err == nil || !strings.Contains(err.Error(), "post not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}
