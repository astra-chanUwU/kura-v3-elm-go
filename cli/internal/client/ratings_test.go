package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRatingReceiptsAndNoReplay(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		uncertain  bool
		success    bool
	}{
		{"changed", `{"posts":[{"id":"10","version":1,"rating":"safe","changed":true}]}`, 200, false, true},
		{"noop", `{"posts":[{"id":"10","version":0,"rating":"safe","changed":false}]}`, 200, false, true},
		{"missing version", `{"posts":[{"id":"10","rating":"safe","changed":true}]}`, 200, true, false},
		{"null changed", `{"posts":[{"id":"10","version":1,"rating":"safe","changed":null}]}`, 200, true, false},
		{"missing posts", `{}`, 200, true, false},
		{"null posts", `{"posts":null}`, 200, true, false},
		{"wrong id", `{"posts":[{"id":"11","version":1,"rating":"safe","changed":true}]}`, 200, true, false},
		{"wrong rating", `{"posts":[{"id":"10","version":1,"rating":"explicit","changed":true}]}`, 200, true, false},
		{"wrong version", `{"posts":[{"id":"10","version":8,"rating":"safe","changed":true}]}`, 200, true, false},
		{"duplicate id", `{"posts":[{"id":"10","version":1,"rating":"safe","changed":true},{"id":"10","version":1,"rating":"safe","changed":true}]}`, 200, true, false},
		{"server error", `{"error":"commit acknowledgement lost"}`, 500, true, false},
		{"stale", `{"error":"rating conflict"}`, 409, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/posts/ratings" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				var targets []map[string]json.RawMessage
				if err := json.Unmarshal(payload["posts"], &targets); err != nil {
					t.Fatal(err)
				}
				if len(targets) != 1 || string(targets[0]["version"]) != "0" {
					t.Errorf("explicit version zero missing: %s", payload["posts"])
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			result, err := New(server.URL).EditRatings(context.Background(), RatingRequest{Posts: []RatingTarget{{ID: "10", Version: 0}}, Rating: "safe"})
			if (err == nil) != test.success {
				t.Fatalf("success=%v error=%v", test.success, err)
			}
			if test.success && (len(result.Posts) != 1 || result.Posts[0].Rating != "safe") {
				t.Fatalf("bad result: %#v", result)
			}
			var requestErr *RequestError
			if test.uncertain && (!errors.As(err, &requestErr) || !requestErr.Uncertain) {
				t.Fatalf("missing uncertain marker: %v", err)
			}
			if !test.uncertain && errors.As(err, &requestErr) && requestErr.Uncertain {
				t.Fatalf("known outcome marked uncertain: %v", err)
			}
			if calls != 1 {
				t.Fatalf("write replayed: %d requests", calls)
			}
		})
	}
}

func TestRatingClientRejectsInvalidInputWithoutRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	for _, request := range []RatingRequest{
		{Posts: []RatingTarget{{ID: "10", Version: 0}}, Rating: "unknown"},
		{Rating: "safe"},
		{Posts: []RatingTarget{{ID: " 10", Version: 0}}, Rating: "safe"},
		{Posts: []RatingTarget{{ID: "10", Version: -1}}, Rating: "safe"},
		{Posts: []RatingTarget{{ID: "10", Version: 0}, {ID: "10", Version: 0}}, Rating: "safe"},
		{Posts: make([]RatingTarget, 101), Rating: "safe"},
	} {
		if _, err := New(server.URL).EditRatings(context.Background(), request); err == nil {
			t.Fatalf("invalid input accepted: %#v", request)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid input made %d requests", calls)
	}
}

func TestRatingClientReadsIndependentVersionAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"10","rating":"explicit","rating_version":3,"reaction_version":8,"rating_history":[{"version":3,"previous_rating":"safe","rating":"explicit","created_at":"now"}]}`)
	}))
	defer server.Close()
	detail, err := New(server.URL).GetPostDetail(context.Background(), "10")
	if err != nil || detail.RatingVersion == nil || *detail.RatingVersion != 3 || detail.ReactionVersion != 8 || len(detail.RatingHistory) != 1 || detail.RatingHistory[0].PreviousRating != "safe" {
		t.Fatalf("detail=%#v err=%v", detail, err)
	}
}
