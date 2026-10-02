package client

import (
	"context"
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
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	res, err := c.Search(context.Background(), "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(res.Posts) != 0 {
		t.Fatalf("expected empty posts, got %d", len(res.Posts))
	}
	if res.NextCursor != nil {
		t.Fatalf("expected nil cursor, got %v", *res.NextCursor)
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
