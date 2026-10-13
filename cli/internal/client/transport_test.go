package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTransportRejectsAmbiguousURLs(t *testing.T) {
	for _, base := range []string{"", "/api", "localhost:8080", "ftp://localhost", "http:///api", "http://user:secret@localhost", "http://localhost?q=x", "http://localhost?", "http://localhost#x", "http://localhost:70000"} {
		if err := ValidateBaseURL(base); err == nil {
			t.Errorf("accepted %q", base)
		}
	}
	for _, base := range []string{"http://localhost:8080", "https://example.com/kura", "http://[::1]:8080/"} {
		if err := ValidateBaseURL(base); err != nil {
			t.Errorf("rejected %q: %v", base, err)
		}
	}
}

func TestTransportEscapesBasePrefixAndIDOnce(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
		fmt.Fprint(w, `{"posts":[],"collection_version":4}`)
	}))
	defer srv.Close()
	result, err := New(srv.URL+"/kura%20space/").CollectionPostsPage(context.Background(), "a/b %?", "opaque+&=/", 3)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/kura%20space/api/collections/a%2Fb%20%25%3F/posts" || gotQuery != "cursor=opaque%2B%26%3D%2F&limit=3" {
		t.Fatalf("path=%q query=%q", gotPath, gotQuery)
	}
	if result.CollectionVersion == nil || *result.CollectionVersion != 4 {
		t.Fatalf("version: %+v", result)
	}
}

func TestTransportRejectsWrongServiceAndOversizedJSON(t *testing.T) {
	for _, body := range []string{"<html>ReportMaker</html>", "null", "[]", `{"status":"ok"}`, `{"posts":[]} trailing`, `{"posts":[],"padding":"` + strings.Repeat("x", maxResponseBytes) + `"}`} {
		t.Run(fmt.Sprintf("size-%d", len(body)), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer srv.Close()
			if _, err := New(srv.URL).Search(context.Background(), ""); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestTransportRejectsEmptyRequiredID(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		call func(*Client) error
	}{
		{"empty collection id", `{"id":"","name":"x","post_ids":[]}`, func(c *Client) error {
			_, err := c.CreateCollection(context.Background(), CreateCollectionRequest{Name: "x"})
			return err
		}},
		{"null detail id", `{"id":null}`, func(c *Client) error { _, err := c.GetPostDetail(context.Background(), "7"); return err }},
		{"blank detail id", `{"id":"  "}`, func(c *Client) error { _, err := c.GetPostDetail(context.Background(), "7"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			err := tc.call(New(srv.URL))
			if err == nil || !strings.Contains(err.Error(), "check --api-url") {
				t.Fatalf("accepted %s: err=%v", tc.body, err)
			}
			var requestErr *RequestError
			if tc.name == "empty collection id" && (!errors.As(err, &requestErr) || !requestErr.Uncertain) {
				t.Fatalf("write validation should be uncertain: err=%v", err)
			}
		})
	}
}

func TestTransportErrorCodeAndCredentialRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"collection changed secret\nplease refresh","code":"collection_changed"}`)
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.APIToken = "secret"
	_, err := c.CollectionPostsPage(context.Background(), "7", "old", 60)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != "collection_changed" {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("unsafe diagnostic %q", err)
	}
}

func TestTransportDoesNotReplayRedirectedWrites(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/destination" {
					t.Error("redirect followed")
				}
				http.Redirect(w, r, "/destination", status)
			}))
			defer srv.Close()
			c := New(srv.URL)
			c.APIToken = "secret"
			_, err := c.EditReactions(context.Background(), ReactionRequest{})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != status || requests != 1 {
				t.Fatalf("requests=%d err=%v", requests, err)
			}
		})
	}
}

func TestWriteResponseFailureIsUncertainAndNeverRetried(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if broken {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Fatal(err)
					}
					conn.Close()
					return
				}
				fmt.Fprint(w, `{"unrelated":true}`)
			}))
			defer srv.Close()
			_, err := New(srv.URL).CreateCollection(context.Background(), CreateCollectionRequest{Name: "test"})
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || !requestErr.Uncertain || requests.Load() != 1 {
				t.Fatalf("requests=%d err=%v", requests.Load(), err)
			}
		})
	}
}

func TestTransportHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New("http://127.0.0.1:1").Search(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRevisionAndDerivativeJSONRoundTrip(t *testing.T) {
	input := `{"id":"7","history":[{"target_tags":null,"revertible":false},{"target_tags":[],"revertible":true},{"target_tags":["cat"]}],"derivative_job_id":"job-1","derivative_status":"succeeded"}`
	var detail PostDetail
	if err := json.Unmarshal([]byte(input), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.History[0].TargetTags != nil || detail.History[0].Revertible || detail.History[1].TargetTags == nil || len(*detail.History[1].TargetTags) != 0 || !detail.History[1].Revertible || !detail.History[2].Revertible {
		t.Fatalf("lost target semantics: %+v", detail.History)
	}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"target_tags":null`, `"target_tags":[]`, `"revertible":true`, `"derivative_job_id":"job-1"`, `"derivative_status":"succeeded"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
}
