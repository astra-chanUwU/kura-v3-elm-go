package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostRatingHelpAndInvalidFlagsMakeNoRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0},
		{[]string{"10", "safe"}, 1},
		{[]string{"--version", "-1", "10", "safe"}, 1},
		{[]string{"--version", "0", "10", "bogus"}, 1},
		{[]string{"--version", "0", "--json", "--jsonl", "10", "safe"}, 1},
		{[]string{"--version", "0", "--json=true", "10", "safe"}, 1},
		{[]string{"--version", "0", "0", "safe"}, 1},
		{[]string{"--version", "2147483648", "10", "safe"}, 1},
		{[]string{"--version", "0", "10"}, 1},
	} {
		args := append([]string{"--api-url", server.URL}, test.args...)
		var out, errors bytes.Buffer
		if got := runPostRating(context.Background(), args, &out, &errors, nil); got != test.code {
			t.Fatalf("args=%v code=%d want=%d error=%s", test.args, got, test.code, errors.String())
		}
	}
	if calls != 0 {
		t.Fatalf("validation/help made %d requests", calls)
	}
}

func TestPostRatingCommandExplicitZeroAndJSONL(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct {
			Posts []struct {
				ID      string `json:"id"`
				Version *int   `json:"version"`
			}
			Rating string `json:"rating"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Posts) != 1 || payload.Posts[0].ID != "10" || payload.Posts[0].Version == nil || *payload.Posts[0].Version != 0 || payload.Rating != "questionable" {
			t.Fatalf("request=%#v", payload)
		}
		fmt.Fprint(w, `{"posts":[{"id":"10","version":1,"rating":"questionable","changed":true}]}`)
	}))
	defer server.Close()
	var out, errors bytes.Buffer
	code := runPost(context.Background(), []string{"rating", "--api-url", server.URL, "--version=0", "--jsonl", "10", "questionable"}, &out, &errors, nil)
	if code != 0 || strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), `"rating":"questionable"`) || calls != 1 {
		t.Fatalf("code=%d stdout=%s stderr=%s calls=%d", code, out.String(), errors.String(), calls)
	}
}
