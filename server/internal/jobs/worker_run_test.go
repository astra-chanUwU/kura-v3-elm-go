package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedClaimStore is a ClaimStore double with scripted claim errors and a
// claim counter, so tests can pin retry bounds and backoff spacing.
type scriptedClaimStore struct {
	claimErrs []error
	calls     int
	claimed   Job
	hasClaim  bool
	succeeded []string
	failed    []string
}

func (s *scriptedClaimStore) Claim(context.Context, string, time.Duration, ...string) (Job, bool, error) {
	s.calls++
	if len(s.claimErrs) > 0 {
		err := s.claimErrs[0]
		s.claimErrs = s.claimErrs[1:]
		return Job{}, false, err
	}
	return s.claimed, s.hasClaim, nil
}

func (s *scriptedClaimStore) RenewLease(_ context.Context, id, _ string, _ time.Duration) (Job, error) {
	job := s.claimed
	job.ID = id
	return job, nil
}

func (s *scriptedClaimStore) Succeed(_ context.Context, id, _ string) (Job, error) {
	s.succeeded = append(s.succeeded, id)
	job := s.claimed
	job.ID = id
	job.Status = StatusSucceeded
	return job, nil
}

func (s *scriptedClaimStore) Fail(_ context.Context, id, _, _ string) (Job, error) {
	s.failed = append(s.failed, id)
	job := s.claimed
	job.ID = id
	job.Status = StatusPending
	return job, nil
}

// TestRunRetriesTransientStoreErrors pins bounded retry: a database that
// fails claims transiently and then recovers is ridden through, and the
// claimed job still completes under the same Run.
func TestRunRetriesTransientStoreErrors(t *testing.T) {
	transient := errors.New("connection refused")
	store := &scriptedClaimStore{
		claimErrs: []error{transient, transient, transient},
		claimed:   Job{ID: "21", Status: StatusRunning},
		hasClaim:  true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker, err := NewWorker(WorkerConfig{
		Store:                store,
		Handler:              func(ctx context.Context, _ Job) error { cancel(); return nil },
		LockedBy:             "worker-retry-test",
		Lease:                time.Minute,
		PollInterval:         5 * time.Millisecond,
		MaxConsecutiveErrors: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run should ride out transient errors: %v", err)
	}
	if store.calls != 4 {
		t.Fatalf("calls=%d want 4 (3 failures + 1 claim)", store.calls)
	}
	if len(store.succeeded) != 1 {
		t.Fatalf("claimed job did not complete: %+v", store)
	}
}

// TestRunSurfacesPersistentStoreOutage pins the lifecycle decision: a
// database that never recovers stops the worker with an error (so the
// process stops accepting uploads and restarts) after a bounded number of
// spaced-out retries, not a busy loop and not forever.
func TestRunSurfacesPersistentStoreOutage(t *testing.T) {
	store := &scriptedClaimStore{claimErrs: []error{
		errors.New("connection refused"),
		errors.New("connection refused"),
		errors.New("connection refused"),
		errors.New("connection refused"),
		errors.New("connection refused"),
	}}
	worker, err := NewWorker(WorkerConfig{
		Store:                store,
		Handler:              func(context.Context, Job) error { return nil },
		LockedBy:             "worker-outage-test",
		Lease:                time.Minute,
		PollInterval:         20 * time.Millisecond,
		MaxConsecutiveErrors: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	runErr := worker.Run(ctx)
	elapsed := time.Since(start)
	if runErr == nil {
		t.Fatal("Run should report a persistent store outage")
	}
	if store.calls != 4 {
		t.Fatalf("calls=%d want 4 (bounded retries, then give up)", store.calls)
	}
	// Backoff after failures 1-3 is 20ms, 40ms, 80ms: 140ms total. Anything
	// far below that is a busy retry loop.
	if elapsed < 100*time.Millisecond {
		t.Fatalf("retries were not backed off: elapsed=%v", elapsed)
	}
}

// TestRunIgnoresLeaseConflicts pins that per-job lease outcomes are not
// outages: repeated lease conflicts keep the worker polling instead of
// counting toward the fatal error bound.
func TestRunIgnoresLeaseConflicts(t *testing.T) {
	store := &scriptedClaimStore{
		claimErrs: []error{ErrLeaseConflict, ErrLeaseConflict},
		claimed:   Job{ID: "22", Status: StatusRunning},
		hasClaim:  true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker, err := NewWorker(WorkerConfig{
		Store:                store,
		Handler:              func(context.Context, Job) error { cancel(); return nil },
		LockedBy:             "worker-conflict-test",
		Lease:                time.Minute,
		PollInterval:         5 * time.Millisecond,
		MaxConsecutiveErrors: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("lease conflicts should not fail the worker: %v", err)
	}
	if len(store.succeeded) != 1 {
		t.Fatalf("claimed job did not complete: %+v", store)
	}
}

// TestWorkerRecoversRestartBeforeLeaseExpiry pins ongoing recovery against
// real PostgreSQL: a derivative job claimed by a dead worker is resumed by
// a restarted worker WITHOUT another restart, even though the restart
// happened before the old lease expired. A non-derivative expired row is
// left alone, pinning the derivative-only scope.
func TestWorkerRecoversRestartBeforeLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t, ctx, testDatabaseURL(t))
	store := NewStore(pool)

	derivative, err := store.Enqueue(ctx, EnqueueRequest{
		Kind:           "derivative",
		Payload:        []byte(`{"post_id":"1","variant":"thumb-320"}`),
		IdempotencyKey: "restart-recovery:derivative",
		MaxAttempts:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The crashed predecessor holds the lease; the restart happens now,
	// BEFORE this lease expires, so startup recovery sees nothing to do.
	if _, _, err := store.Claim(ctx, "dead-worker", 400*time.Millisecond, "derivative"); err != nil {
		t.Fatal(err)
	}

	other, err := store.Enqueue(ctx, EnqueueRequest{
		Kind:           "export",
		Payload:        []byte(`{}`),
		IdempotencyKey: "restart-recovery:export",
		MaxAttempts:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Claim(ctx, "other-worker", time.Millisecond, "export"); err != nil {
		t.Fatal(err)
	}
	// Let the other-kind lease lapse while the restarted worker runs.
	time.Sleep(50 * time.Millisecond)

	worker, err := NewWorker(WorkerConfig{
		Store:                store,
		Handler:              func(context.Context, Job) error { return nil },
		LockedBy:             "restarted-worker",
		Lease:                5 * time.Second,
		PollInterval:         20 * time.Millisecond,
		RecoveryInterval:     20 * time.Millisecond,
		MaxConsecutiveErrors: 50,
		Kinds:                []string{"derivative"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()
	deadline := time.Now().Add(25 * time.Second)
	for {
		job, err := store.GetJob(ctx, derivative.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == StatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("derivative job never completed after restart: %#v", job)
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("worker Run: %v", err)
	}

	untouched, err := store.GetJob(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Status != StatusRunning || untouched.LockedBy == nil || *untouched.LockedBy != "other-worker" {
		t.Fatalf("non-derivative row disturbed by derivative recovery: %#v", untouched)
	}
}
