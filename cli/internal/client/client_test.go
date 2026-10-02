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
