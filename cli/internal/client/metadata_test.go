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

func TestMetadataReceiptsAndBody(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		uncertain  bool
		success    bool
	}{
		{"changed", `{"posts":[{"id":"10","version":1,"source":"new","artist":"old","changed":true}]}`, 200, false, true},
		{"noop", `{"posts":[{"id":"10","version":0,"source":"new","artist":"old","changed":false}]}`, 200, false, true},
		{"missing source", `{"posts":[{"id":"10","version":1,"artist":"old","changed":true}]}`, 200, true, false},
		{"null changed", `{"posts":[{"id":"10","version":1,"source":"new","artist":"old","changed":null}]}`, 200, true, false},
		{"null posts", `{"posts":null}`, 200, true, false},
		{"wrong id", `{"posts":[{"id":"11","version":1,"source":"new","artist":"old","changed":true}]}`, 200, true, false},
		{"duplicate ack", `{"posts":[{"id":"10","version":1,"source":"new","artist":"old","changed":true},{"id":"10","version":1,"source":"new","artist":"old","changed":true}]}`, 200, true, false},
		{"wrong version", `{"posts":[{"id":"10","version":3,"source":"new","artist":"old","changed":true}]}`, 200, true, false},
		{"wrong source", `{"posts":[{"id":"10","version":1,"source":"other","artist":"old","changed":true}]}`, 200, true, false},
		{"server error", `{"error":"lost"}`, 500, true, false},
		{"conflict", `{"error":"stale"}`, 409, false, false},
		{"hidden", `{"error":"post not found"}`, 404, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/posts/metadata" {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				var targets []map[string]json.RawMessage
				if err := json.Unmarshal(payload["posts"], &targets); err != nil || len(targets) != 1 || string(targets[0]["version"]) != "0" {
					t.Errorf("version zero body %s err %v", payload["posts"], err)
				}
				if string(payload["source"]) != `"new"` {
					t.Errorf("source body %s", payload["source"])
				}
				if _, ok := payload["artist"]; ok {
					t.Errorf("omitted artist sent: %s", payload["artist"])
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer srv.Close()
			source := "  new  "
			result, err := New(srv.URL).EditMetadata(context.Background(), MetadataRequest{Posts: []MetadataTarget{{ID: "10", Version: 0}}, Source: &source})
			if (err == nil) != test.success {
				t.Fatalf("success=%v err=%v", test.success, err)
			}
			if test.success && (len(result.Posts) != 1 || result.Posts[0].Source != "new") {
				t.Fatalf("result %#v", result)
			}
			var reqErr *RequestError
			if test.uncertain && (!errors.As(err, &reqErr) || !reqErr.Uncertain) {
				t.Fatalf("want uncertain: %v", err)
			}
			if !test.uncertain && errors.As(err, &reqErr) && reqErr.Uncertain {
				t.Fatalf("unexpected uncertain: %v", err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestMetadataExplicitClearAndInvalidWithoutRequests(t *testing.T) {
	calls := 0
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		data := make([]byte, 4096)
		n, _ := r.Body.Read(data)
		raw = string(data[:n])
		fmt.Fprint(w, `{"posts":[{"id":"10","version":1,"source":"","artist":"a","changed":true}]}`)
	}))
	defer srv.Close()
	empty := ""
	if _, err := New(srv.URL).EditMetadata(context.Background(), MetadataRequest{Posts: []MetadataTarget{{ID: "10", Version: 0}}, Source: &empty}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"source":""`) || strings.Contains(raw, `"artist"`) {
		t.Fatalf("clear body %s", raw)
	}
	calls = 0
	tooLong := strings.Repeat("é", 513)
	withNUL := "a\x00b"
	invalidUTF8 := "\xff"
	for _, req := range []MetadataRequest{
		{Posts: []MetadataTarget{{ID: "10", Version: 0}}},
		{Posts: []MetadataTarget{{ID: "010", Version: 0}}, Source: &empty},
		{Posts: []MetadataTarget{{ID: "10", Version: -1}}, Source: &empty},
		{Posts: []MetadataTarget{{ID: "10", Version: 0}, {ID: "11", Version: 0}}, Source: &empty},
		{Posts: []MetadataTarget{{ID: "10", Version: 0}}, Source: &tooLong},
		{Posts: []MetadataTarget{{ID: "10", Version: 0}}, Artist: &withNUL},
		{Posts: []MetadataTarget{{ID: "10", Version: 0}}, Artist: &invalidUTF8},
	} {
		if _, err := New(srv.URL).EditMetadata(context.Background(), req); err == nil {
			t.Fatalf("accepted %#v", req)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid made %d calls", calls)
	}
}
