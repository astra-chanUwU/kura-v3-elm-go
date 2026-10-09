package jobs

import (
	"context"
	"strings"
	"time"
)

// Handler executes one claimed job. A nil return succeeds the row; a
// non-nil return fails it with retry/backoff. Handlers must respect ctx
// cancellation and must not touch uploads or derivatives: no kind is wired
// here, the worker only moves rows through the lease lifecycle.
type Handler func(ctx context.Context, job Job) error

// ClaimStore is the worker's storage boundary. Store implements it; tests
// substitute a fake.
type ClaimStore interface {
	Claim(ctx context.Context, lockedBy string, lease time.Duration, kinds ...string) (Job, bool, error)
	RenewLease(ctx context.Context, id, lockedBy string, lease time.Duration) (Job, error)
	Succeed(ctx context.Context, id, lockedBy string) (Job, error)
	Fail(ctx context.Context, id, lockedBy, errMsg string) (Job, error)
}

// Worker runs jobs in-process: claim one due row, renew its lease while the
// handler runs, then succeed or fail it. There is no Redis, broker, or
// external deployment piece; callers run exactly one worker per process.
type Worker struct {
	store        ClaimStore
	handler      Handler
	lockedBy     string
	lease        time.Duration
	pollInterval time.Duration
	kinds        []string
}

// WorkerConfig tunes a Worker. LockedBy identifies this worker on claimed
// rows; Lease bounds each claim; PollInterval spaces out empty polls; Kinds
// optionally restricts claims to job kinds (empty claims any kind).
type WorkerConfig struct {
	Store        ClaimStore
	Handler      Handler
	LockedBy     string
	Lease        time.Duration
	PollInterval time.Duration
	Kinds        []string
}

// NewWorker validates config and returns a worker. Zero Lease defaults to 30
// seconds and zero PollInterval to 1 second.
func NewWorker(config WorkerConfig) (*Worker, error) {
	if config.Store == nil || config.Handler == nil {
		return nil, ErrInvalidJob
	}
	lockedBy := strings.TrimSpace(config.LockedBy)
	if lockedBy == "" {
		return nil, ErrInvalidJob
	}
	lease := config.Lease
	if lease <= 0 {
		lease = 30 * time.Second
	}
	pollInterval := config.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &Worker{
		store:        config.Store,
		handler:      config.Handler,
		lockedBy:     lockedBy,
		lease:        lease,
		pollInterval: pollInterval,
		kinds:        append([]string(nil), config.Kinds...),
	}, nil
}

// Run claims and executes jobs until ctx is done, then returns nil. Empty
// polls wait PollInterval; handler work runs with lease renewal in the
// background so long jobs keep their row.
func (w *Worker) Run(ctx context.Context) error {
	if w == nil {
		return ErrUnavailable
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		worked, err := w.RunOnce(ctx)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(w.pollInterval):
		}
	}
}

// RunOnce claims a single due job and handles it, reporting whether a job
// was processed. Claim misses are not errors.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil {
		return false, ErrUnavailable
	}
	job, claimed, err := w.store.Claim(ctx, w.lockedBy, w.lease, w.kinds...)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	handlerCtx, stopRenew := context.WithCancel(ctx)
	defer stopRenew()
	go w.renewLoop(handlerCtx, job.ID)
	if handlerErr := w.handler(handlerCtx, job); handlerErr != nil {
		if _, failErr := w.store.Fail(ctx, job.ID, w.lockedBy, handlerErr.Error()); failErr != nil {
			return true, failErr
		}
		return true, nil
	}
	if _, succeedErr := w.store.Succeed(ctx, job.ID, w.lockedBy); succeedErr != nil {
		return true, succeedErr
	}
	return true, nil
}

// renewLoop extends the row lease until handlerCtx ends. Renewal errors
// mean the lease is lost, so the loop stops and the handler's eventual
// Succeed/Fail surfaces the conflict.
func (w *Worker) renewLoop(ctx context.Context, jobID string) {
	interval := w.lease / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := w.store.RenewLease(renewCtx, jobID, w.lockedBy, w.lease)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
