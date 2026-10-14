package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTagManagementRequestsAndProtectedReadAuth(t *testing.T) {
	var gotPreview, gotApply bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cli-secret" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags/manage":
			if r.URL.Query().Get("prefix") != "cat" || r.URL.Query().Get("limit") != "60" || r.URL.Query().Get("cursor") != "opaque+cursor" {
				t.Errorf("list query = %q", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"version":12,"tags":[{"tag":"cat","post_count":5,"visible_post_count":3}],"next_cursor":"next"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/tags/manage/preview":
			gotPreview = true
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 3 || string(body["mode"]) != `"rename"` || string(body["source_tag"]) != `"cat"` || string(body["target_tag"]) != `"feline"` {
				t.Errorf("preview body = %s", mustJSON(body))
			}
			fmt.Fprint(w, `{"version":12,"mode":"rename","source_tag":"cat","target_tag":"feline","affected_post_count":5,"visible_post_count":3,"target_post_count":0}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/tags/manage/apply":
			gotApply = true
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if string(body["expected_version"]) != "12" || string(body["source_tag"]) != `"cat"` || string(body["target_tag"]) != `"feline"` {
				t.Errorf("apply body = %s", mustJSON(body))
			}
			fmt.Fprint(w, `{"id":"13","version":13,"mode":"rename","source_tag":"cat","target_tag":"feline","affected_post_count":5,"visible_post_count":3,"changed":true}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.APIToken = "cli-secret"

	listed, err := c.ListTagsPage(context.Background(), "cat", "opaque+cursor", 60)
	if err != nil || listed.Version != 12 || len(listed.Tags) != 1 || listed.NextCursor == nil || *listed.NextCursor != "next" {
		t.Fatalf("list = %+v, err=%v", listed, err)
	}
	preview, err := c.PreviewTagManage(context.Background(), "rename", "cat", "feline")
	if err != nil || preview.Version != 12 || preview.AffectedPostCount != 5 {
		t.Fatalf("preview = %+v, err=%v", preview, err)
	}
	applied, err := c.ApplyTagManage(context.Background(), "rename", " cat ", " feline ", 12)
	if err != nil || applied.ID != "13" || applied.Version != 13 || !applied.Changed {
		t.Fatalf("apply = %+v, err=%v", applied, err)
	}
	if !gotPreview || !gotApply {
		t.Fatalf("preview=%v apply=%v", gotPreview, gotApply)
	}
}

func TestTagManagementRejectsInvalidInputsBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer srv.Close()
	c := New(srv.URL)
	tooLong := strings.Repeat("x", 129)
	for _, input := range []struct{ source, target string }{
		{" ", "target"},
		{"same", " same "},
		{"bad\x00tag", "target"},
		{string([]byte{0xff}), "target"},
		{tooLong, "target"},
	} {
		if _, err := c.PreviewTagManage(context.Background(), "rename", input.source, input.target); err == nil {
			t.Errorf("accepted source=%q target=%q", input.source, input.target)
		}
	}
	if _, err := c.ApplyTagManage(context.Background(), "rename", "source", "target", math.MaxInt64); err == nil {
		t.Fatal("accepted an expected version that cannot be incremented")
	}
	if requests != 0 {
		t.Fatalf("invalid inputs sent %d requests", requests)
	}
}

func TestTagManagementRejectsMalformedOrStaleApplyReceipts(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"noncanonical id", `{"id":"01","version":6,"mode":"merge","source_tag":"cat","target_tag":"feline","affected_post_count":2,"visible_post_count":1,"changed":true}`},
		{"stale version", `{"id":"1","version":7,"mode":"merge","source_tag":"cat","target_tag":"feline","affected_post_count":2,"visible_post_count":1,"changed":true}`},
		{"unchanged", `{"id":"1","version":6,"mode":"merge","source_tag":"cat","target_tag":"feline","affected_post_count":2,"visible_post_count":1,"changed":false}`},
		{"empty affected set", `{"id":"1","version":6,"mode":"merge","source_tag":"cat","target_tag":"feline","affected_post_count":0,"visible_post_count":0,"changed":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := New(srv.URL)
			_, err := c.ApplyTagManage(context.Background(), "merge", "cat", "feline", 5)
			requestErr, ok := err.(*RequestError)
			if !ok || !requestErr.Uncertain || requests != 1 {
				t.Fatalf("err=%v requests=%d", err, requests)
			}
		})
	}
}

func TestTagManagementConflictIsNotRetried(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"taxonomy version changed"}`)
	}))
	defer srv.Close()
	c := New(srv.URL)
	_, err := c.ApplyTagManage(context.Background(), "merge", "cat", "feline", 4)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusConflict || requests != 1 {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
}

func mustJSON(value any) string {
	b, _ := json.Marshal(value)
	return string(b)
}
