package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestValidateEnqueueDefaults(t *testing.T) {
	validated, err := ValidateEnqueue(EnqueueRequest{Kind: "  export  ", IdempotencyKey: " key-1 "})
	if err != nil {
		t.Fatal(err)
	}
	if validated.Kind != "export" || validated.IdempotencyKey != "key-1" {
		t.Fatalf("inputs not trimmed: %#v", validated)
	}
	if string(validated.Payload) != "{}" {
		t.Fatalf("payload not defaulted: %s", validated.Payload)
	}
	if validated.MaxAttempts != defaultMaxAttempts {
		t.Fatalf("max attempts not defaulted: %d", validated.MaxAttempts)
	}
}

func TestValidateEnqueueRejects(t *testing.T) {
	for _, request := range []EnqueueRequest{
		{Kind: "", IdempotencyKey: "k"},
		{Kind: "k", IdempotencyKey: ""},
		{Kind: "k", IdempotencyKey: "k", Payload: json.RawMessage("{invalid}")},
		{Kind: "k", IdempotencyKey: "k", MaxAttempts: 101},
	} {
		if _, err := ValidateEnqueue(request); !errors.Is(err, ErrInvalidJob) {
			t.Errorf("%#v: got %v, want ErrInvalidJob", request, err)
		}
	}
}

func TestParseID(t *testing.T) {
	id, err := ParseID("42")
	if err != nil || id != "42" {
		t.Fatalf("valid id rejected: %q %v", id, err)
	}
	for _, raw := range []string{"", "0", "-3", "0042", "abc", "4.2"} {
		if _, err := ParseID(raw); !errors.Is(err, ErrInvalidJob) {
			t.Errorf("%q: got %v, want ErrInvalidJob", raw, err)
		}
	}
}

func TestRetryDelayBackoff(t *testing.T) {
	if RetryDelay(1) != 5*time.Second {
		t.Fatalf("attempt 1: got %v", RetryDelay(1))
	}
	if RetryDelay(2) != 10*time.Second || RetryDelay(3) != 20*time.Second {
		t.Fatalf("backoff not doubling: %v %v", RetryDelay(2), RetryDelay(3))
	}
	if RetryDelay(0) != 5*time.Second {
		t.Fatalf("attempt 0: got %v", RetryDelay(0))
	}
	if RetryDelay(100) != 10*time.Minute {
		t.Fatalf("backoff not capped: got %v", RetryDelay(100))
	}
	previous := time.Duration(0)
	for attempt := 1; attempt <= 8; attempt++ {
		delay := RetryDelay(attempt)
		if delay <= previous {
			t.Fatalf("attempt %d not increasing: %v <= %v", attempt, delay, previous)
		}
		previous = delay
	}
}

func TestIsTerminal(t *testing.T) {
	for _, status := range []string{StatusSucceeded, StatusFailed, StatusCancelled} {
		if !IsTerminal(status) {
			t.Errorf("%s should be terminal", status)
		}
	}
	for _, status := range []string{StatusPending, StatusRunning, "other"} {
		if IsTerminal(status) {
			t.Errorf("%s should not be terminal", status)
		}
	}
}

// fakeClaimStore is the worker's storage double: scripted claims with a log
// of terminal transitions.
type fakeClaimStore struct {
	claimed  Job
	hasClaim bool
	claimErr error

	renewed    int
	succeeded  []string
	failed     []string
	failErr    error
	succeedErr error
}

func (f *fakeClaimStore) Claim(context.Context, string, time.Duration, ...string) (Job, bool, error) {
	if f.claimErr != nil {
		return Job{}, false, f.claimErr
	}
	return f.claimed, f.hasClaim, nil
}

func (f *fakeClaimStore) RenewLease(_ context.Context, id, _ string, _ time.Duration) (Job, error) {
	f.renewed++
	job := f.claimed
	job.ID = id
	return job, nil
}

func (f *fakeClaimStore) Succeed(_ context.Context, id, _ string) (Job, error) {
	if f.succeedErr != nil {
		return Job{}, f.succeedErr
	}
	f.succeeded = append(f.succeeded, id)
	job := f.claimed
	job.ID = id
	job.Status = StatusSucceeded
	return job, nil
}

func (f *fakeClaimStore) Fail(_ context.Context, id, _, _ string) (Job, error) {
	if f.failErr != nil {
		return Job{}, f.failErr
	}
	f.failed = append(f.failed, id)
	job := f.claimed
	job.ID = id
	job.Status = StatusPending
	return job, nil
}

func testWorker(t *testing.T, store ClaimStore, handler Handler) *Worker {
	t.Helper()
	worker, err := NewWorker(WorkerConfig{
		Store:    store,
		Handler:  handler,
		LockedBy: "worker-1",
		Lease:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func TestNewWorkerRejectsBadConfig(t *testing.T) {
	handler := func(context.Context, Job) error { return nil }
	if _, err := NewWorker(WorkerConfig{Handler: handler, LockedBy: "w"}); !errors.Is(err, ErrInvalidJob) {
		t.Errorf("nil store: got %v", err)
	}
	if _, err := NewWorker(WorkerConfig{Store: &fakeClaimStore{}, LockedBy: "w"}); !errors.Is(err, ErrInvalidJob) {
		t.Errorf("nil handler: got %v", err)
	}
	if _, err := NewWorker(WorkerConfig{Store: &fakeClaimStore{}, Handler: handler}); !errors.Is(err, ErrInvalidJob) {
		t.Errorf("blank owner: got %v", err)
	}
}

func TestRunOnceSucceedsClaimedJob(t *testing.T) {
	store := &fakeClaimStore{claimed: Job{ID: "7", Status: StatusRunning}, hasClaim: true}
	handled := false
	worker := testWorker(t, store, func(_ context.Context, job Job) error {
		handled = true
		if job.ID != "7" {
			t.Errorf("handler got job %q", job.ID)
		}
		return nil
	})
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked || !handled {
		t.Fatalf("worked=%v handled=%v err=%v", worked, handled, err)
	}
	if len(store.succeeded) != 1 || store.succeeded[0] != "7" || len(store.failed) != 0 {
		t.Fatalf("unexpected transitions: %+v", store)
	}
}

func TestRunOnceFailsClaimedJob(t *testing.T) {
	store := &fakeClaimStore{claimed: Job{ID: "9", Status: StatusRunning}, hasClaim: true}
	worker := testWorker(t, store, func(context.Context, Job) error {
		return errors.New("boom")
	})
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if len(store.failed) != 1 || len(store.succeeded) != 0 {
		t.Fatalf("unexpected transitions: %+v", store)
	}
}

func TestRunOnceWithoutClaim(t *testing.T) {
	store := &fakeClaimStore{}
	worker := testWorker(t, store, func(context.Context, Job) error {
		t.Error("handler must not run without a claim")
		return nil
	})
	worked, err := worker.RunOnce(context.Background())
	if err != nil || worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	store := &fakeClaimStore{}
	worker := testWorker(t, store, func(context.Context, Job) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("cancelled run: got %v", err)
	}
}
