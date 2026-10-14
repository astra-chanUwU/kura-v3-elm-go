package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrashRestoreBodiesAuthAndReceiptOrder(t *testing.T) {
	var auth, raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		data := make([]byte, 8192)
		n, _ := r.Body.Read(data)
		raw = string(data[:n])
		if r.URL.Path == "/api/posts/trash" {
			fmt.Fprint(w, `{"posts":[{"id":"2","changed":true},{"id":"1","changed":false}]}`)
			return
		}
		fmt.Fprint(w, `{"posts":[{"id":"1","changed":true}]}`)
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.APIToken = "tok"
	reason := "  dup  "
	result, err := c.TrashPosts(context.Background(), []string{"1", "2"}, &reason)
	if err != nil || len(result.Posts) != 2 || result.Posts[0].ID != "2" {
		t.Fatalf("trash %#v err=%v", result, err)
	}
	if auth != "Bearer "+"tok" || !strings.Contains(raw, `"post_ids":["1","2"]`) || !strings.Contains(raw, `"reason":"dup"`) {
		t.Fatalf("auth=%q raw=%s", auth, raw)
	}
	raw = ""
	if _, err := c.RestorePosts(context.Background(), []string{"1"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "reason") {
		t.Fatalf("restore sent reason %s", raw)
	}
}

func TestTrashInvalidReceiptsUncertain(t *testing.T) {
	for _, body := range []string{
		`{"posts":[{"id":"1","changed":true}]}`,
		`{"posts":[{"id":"1","changed":true},{"id":"1","changed":true}]}`,
		`{"posts":[{"id":"1","changed":true},{"id":"3","changed":true}]}`,
		`{"posts":[{"id":"1","changed":true},{"id":"2","changed":null}]}`,
		`{"posts":null}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, err := New(srv.URL).TrashPosts(context.Background(), []string{"1", "2"}, nil)
		srv.Close()
		var reqErr *RequestError
		if !errors.As(err, &reqErr) || !reqErr.Uncertain {
			t.Fatalf("body %s err %v", body, err)
		}
	}
}

func TestTrashInvalidInputWithoutRequests(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{}`) }))
	defer srv.Close()
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprint(i + 1)
	}
	for _, ids := range [][]string{{}, {"01"}, {"1", "1"}, many, {"0"}, {" 1"}} {
		if _, err := New(srv.URL).TrashPosts(context.Background(), ids, nil); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	if calls != 0 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestListTrashProtectedAndNormalized(t *testing.T) {
	var auth, rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		rawQuery = r.URL.RawQuery
		if r.URL.Query().Get("cursor") == "empty" {
			fmt.Fprint(w, `{"posts":null,"next_cursor":null}`)
			return
		}
		fmt.Fprint(w, `{"posts":[{"id":"7","media_type":"image/png","width":2,"height":3,"deleted_at":"2026-01-01","moderation_state":"published"}],"next_cursor":"next"}`)
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.APIToken = "tok"
	result, err := c.ListTrash(context.Background(), "tok", 5)
	if err != nil || len(result.Posts) != 1 || result.Posts[0].DeletedAt == "" || result.NextCursor == nil {
		t.Fatalf("result %#v err=%v", result, err)
	}
	if auth != "Bearer "+"tok" || !strings.Contains(rawQuery, "cursor=tok") || !strings.Contains(rawQuery, "limit=5") {
		t.Fatalf("auth=%q query=%q", auth, rawQuery)
	}
	empty, err := c.ListTrash(context.Background(), "empty", 0)
	if err != nil || empty.Posts == nil || len(empty.Posts) != 0 {
		t.Fatalf("empty %#v err=%v", empty, err)
	}
	// Ensure the request body type used by trash validates JSON shape.
	var decoded TrashRequest
	if err := json.Unmarshal([]byte(`{"post_ids":["1"]}`), &decoded); err != nil || decoded.Reason != nil {
		t.Fatalf("decoded %#v err=%v", decoded, err)
	}
}
