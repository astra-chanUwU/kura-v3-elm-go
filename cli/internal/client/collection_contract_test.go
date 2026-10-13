package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompactCollectionResponseFailureIsUncertain(t *testing.T) {
	for _, body := range []string{
		`{"id":"7","collection_id":"7","version":null,"changed":true}`,
		`{"id":"7","collection_id":"7","version":0,"changed":null}`,
		`{"id":"7","collection_id":"7","version":-1,"changed":true}`,
		`{"id":"7","collection_id":"8","version":1,"changed":true}`,
		`{"id":"8","collection_id":"8","version":1,"changed":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			_, err := New(server.URL).MoveCollectionPost(context.Background(), "7", MoveCollectionPostRequest{PostID: "42", ExpectedVersion: 0})
			var uncertain *RequestError
			if !errors.As(err, &uncertain) || !uncertain.Uncertain {
				t.Fatalf("error=%v, expected uncertain write", err)
			}
		})
	}
}

func TestMalformedCollectionSummariesAreRejected(t *testing.T) {
	for _, body := range []string{
		`{"collections":[{"id":"7","name":"x"}]}`,
		`{"collections":[{"id":"7","name":"x","version":null,"member_count":0,"visible_post_count":0}]}`,
		`{"collections":[{"id":"7","name":"x","version":0,"member_count":0,"visible_post_count":1}]}`,
		`{"collections":[{"id":"07","name":"x","version":0,"member_count":0,"visible_post_count":0}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			if _, err := New(server.URL).ListCollectionSummaries(context.Background()); err == nil {
				t.Fatal("malformed summary accepted")
			}
		})
	}
}
