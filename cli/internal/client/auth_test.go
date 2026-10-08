package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewReadsTrimmedAPIToken(t *testing.T) {
	t.Setenv("KURA_API_TOKEN", "  secret  ")
	if got := New("").APIToken; got != "secret" {
		t.Fatalf("APIToken=%q, want %q", got, "secret")
	}
}

func TestClientAddsBearerToken(t *testing.T) {
	var getAuthorization, postAuthorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getAuthorization = r.Header.Get("Authorization")
		} else {
			postAuthorization = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIToken = "secret"
	if _, err := c.Search(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if getAuthorization != "" {
		t.Fatalf("GET authorization=%q, want empty", getAuthorization)
	}
	if _, err := c.EditReactions(context.Background(), ReactionRequest{}); err != nil {
		t.Fatal(err)
	}
	if postAuthorization != "Bearer secret" {
		t.Fatalf("POST authorization=%q", postAuthorization)
	}
}
