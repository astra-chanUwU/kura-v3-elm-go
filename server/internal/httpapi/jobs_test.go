package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeJobSearcher struct {
	job jobs.Job
	err error
}

func (f *fakeJobSearcher) SearchPosts(context.Context, string, *posts.Cursor, int) (posts.SearchPage, error) {
	return posts.SearchPage{}, nil
}

func (f *fakeJobSearcher) GetJob(_ context.Context, id string) (jobs.Job, error) {
	if f.err != nil {
		return jobs.Job{}, f.err
	}
	job := f.job
	job.ID = id
	return job, nil
}

func jobStatusRequest(t *testing.T, router http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/jobs/"+id, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestGetJobStatus(t *testing.T) {
	fake := &fakeJobSearcher{job: jobs.Job{Kind: "export", Status: jobs.StatusRunning, Progress: 40}}
	router := NewRouter(fake)
	response := jobStatusRequest(t, router, "42")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body jobs.PublicJob
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != "42" || body.Kind != "export" || body.Status != jobs.StatusRunning || body.Progress != 40 || body.Action != jobs.ActionWait {
		t.Fatalf("unexpected job body: %#v", body)
	}
}

func TestGetJobStatusHidesRawFailure(t *testing.T) {
	rawKey := "uploads/s6secretpath.png"
	owner := "worker-s6secretpath"
	fake := &fakeJobSearcher{job: jobs.Job{
		Kind:           "derivative",
		Payload:        json.RawMessage(`{"original_key":"` + rawKey + `"}`),
		Status:         jobs.StatusFailed,
		IdempotencyKey: "derivative:42:thumb-320",
		Attempts:       25,
		MaxAttempts:    25,
		LastError:      "open original: media object not found: " + rawKey + " password=s6hunter2",
		LockedBy:       &owner,
	}}
	response := jobStatusRequest(t, NewRouter(fake), "42")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, leaked := range []string{"s6secretpath", "s6hunter2", "original_key", "idempotency_key", "locked_by", "payload", "password"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("status response contains %q: %s", leaked, body)
		}
	}
	var public jobs.PublicJob
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public.Action != jobs.ActionOriginalSaved || public.LastError != "The file could not be read." || public.Attempts != 25 {
		t.Fatalf("public failure = %#v", public)
	}
}

func TestGetJobStatusNotFound(t *testing.T) {
	fake := &fakeJobSearcher{err: jobs.ErrNotFound}
	router := NewRouter(fake)
	if response := jobStatusRequest(t, router, "42"); response.Code != http.StatusNotFound {
		t.Fatalf("missing job status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetJobStatusRejectsBadID(t *testing.T) {
	fake := &fakeJobSearcher{}
	router := NewRouter(fake)
	for _, id := range []string{"0", "-3", "abc", "0042"} {
		if response := jobStatusRequest(t, router, id); response.Code != http.StatusNotFound {
			t.Errorf("%s: got status %d", id, response.Code)
		}
	}
}

func TestGetJobStatusUnavailableWithoutReader(t *testing.T) {
	if response := jobStatusRequest(t, NewRouter(), "42"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("no jobs reader: got status %d", response.Code)
	}
	fake := &fakeSearcher{}
	if response := jobStatusRequest(t, NewRouter(fake), "42"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("posts-only searcher: got status %d", response.Code)
	}
}

func TestGetJobStatusMapsStoreErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{jobs.ErrUnavailable, http.StatusServiceUnavailable},
		{jobs.ErrInvalidJob, http.StatusBadRequest},
		{errors.New("db down"), http.StatusInternalServerError},
	} {
		router := NewRouter(&fakeJobSearcher{err: tc.err})
		if response := jobStatusRequest(t, router, "42"); response.Code != tc.status {
			t.Errorf("%v: got status %d, want %d", tc.err, response.Code, tc.status)
		}
	}
}
