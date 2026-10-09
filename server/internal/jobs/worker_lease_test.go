package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

// lostLeaseStore is a ClaimStore double whose lease renewals always fail,
// simulating a reclaimed or expired lease mid-handler.
type lostLeaseStore struct {
	claimed Job
	failed  []string
}

func (f *lostLeaseStore) Claim(context.Context, string, time.Duration, ...string) (Job, bool, error) {
	return f.claimed, true, nil
}

func (f *lostLeaseStore) RenewLease(context.Context, string, string, time.Duration) (Job, error) {
	return Job{}, ErrLeaseConflict
}

func (f *lostLeaseStore) Succeed(context.Context, string, string) (Job, error) {
	return Job{}, ErrLeaseConflict
}

func (f *lostLeaseStore) Fail(_ context.Context, id, _, _ string) (Job, error) {
	f.failed = append(f.failed, id)
	job := f.claimed
	job.ID = id
	job.Status = StatusPending
	return job, nil
}

// TestRunOnceCancelsHandlerOnLeaseLoss pins the crash-safety contract:
// when the lease is lost mid-handler, the worker cancels the handler
// context so processing stops instead of running on after reclaim.
func TestRunOnceCancelsHandlerOnLeaseLoss(t *testing.T) {
	store := &lostLeaseStore{claimed: Job{ID: "11", Status: StatusRunning}}
	sawCancel := make(chan bool, 1)
	worker, err := NewWorker(WorkerConfig{
		Store:        store,
		Handler:      func(ctx context.Context, _ Job) error {
			select {
			case <-ctx.Done():
				sawCancel <- true
				return ctx.Err()
			case <-time.After(10 * time.Second):
				sawCancel <- false
				return nil
			}
		},
		LockedBy:     "worker-lease-test",
		Lease:        30 * time.Millisecond,
		PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !worked {
		t.Fatal("expected claimed work to be reported")
	}
	select {
	case cancelled := <-sawCancel:
		if !cancelled {
			t.Fatal("handler finished without observing lease-loss cancellation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handler was not cancelled after lease loss")
	}
	if len(store.failed) != 1 || store.failed[0] != "11" {
		t.Fatalf("handler error not reported through Fail: %+v", store)
	}
}

// TestRunOnceSurfacesTerminalErrors pins failure surfacing: when the
// terminal transition itself fails, RunOnce reports the error instead of
// silently dropping the job outcome.
func TestRunOnceSurfacesTerminalErrors(t *testing.T) {
	store := &fakeClaimStore{claimed: Job{ID: "12", Status: StatusRunning}, hasClaim: true}
	store.failErr = errors.New("database unreachable")
	worker := testWorker(t, store, func(context.Context, Job) error {
		return errors.New("boom")
	})
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("expected terminal transition failure to surface")
	}
}
