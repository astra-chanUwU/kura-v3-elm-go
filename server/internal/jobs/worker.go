package jobs

import (
	"context"
	"errors"
	"fmt"
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
	store                ClaimStore
	handler              Handler
	lockedBy             string
	lease                time.Duration
	pollInterval         time.Duration
	kinds                []string
	recoveryInterval     time.Duration
	maxConsecutiveErrors int
}

// leaseRecoverer is the worker's optional recovery boundary. *Store
// implements it; test doubles need not, and the worker skips background
// recovery when the store lacks it.
type leaseRecoverer interface {
	RecoverExpiredLeasesForKinds(ctx context.Context, kinds ...string) (int64, error)
}

// Bounds for the worker's background loops. Recovery sweeps are single
// UPDATE statements scoped to the worker's claim kinds; error backoff keeps
// a downed database from hot-looping while still failing fast enough for
// the process to restart.
const (
	defaultRecoveryInterval     = 10 * time.Second
	defaultMaxConsecutiveErrors = 10
	maxErrorBackoff             = 10 * time.Second
)

// WorkerConfig tunes a Worker. LockedBy identifies this worker on claimed
// rows; Lease bounds each claim; PollInterval spaces out empty polls; Kinds
// optionally restricts claims to job kinds (empty claims any kind).
// RecoveryInterval spaces out background recovery of expired leases held by
// dead workers, scoped to Kinds; MaxConsecutiveErrors bounds transient
// store failures before Run reports the outage instead of retrying forever.
type WorkerConfig struct {
	Store                ClaimStore
	Handler              Handler
	LockedBy             string
	Lease                time.Duration
	PollInterval         time.Duration
	Kinds                []string
	RecoveryInterval     time.Duration
	MaxConsecutiveErrors int
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
	recoveryInterval := config.RecoveryInterval
	if recoveryInterval <= 0 {
		recoveryInterval = defaultRecoveryInterval
	}
	maxConsecutiveErrors := config.MaxConsecutiveErrors
	if maxConsecutiveErrors <= 0 {
		maxConsecutiveErrors = defaultMaxConsecutiveErrors
	}
	return &Worker{
		store:                config.Store,
		handler:              config.Handler,
		lockedBy:             lockedBy,
		lease:                lease,
		pollInterval:         pollInterval,
		kinds:                append([]string(nil), config.Kinds...),
		recoveryInterval:     recoveryInterval,
		maxConsecutiveErrors: maxConsecutiveErrors,
	}, nil
}

// Run claims and executes jobs until ctx is done, then returns nil. Empty
// polls wait PollInterval; handler work runs with lease renewal in the
// background so long jobs keep their row. Expired leases held by dead
// workers are recovered on RecoveryInterval, scoped to the worker's claim
// kinds, so a restart before the previous lease expires still resumes the
// job once that lease lapses. Transient store failures retry with bounded
// backoff; once they exceed MaxConsecutiveErrors the outage is returned so
// the process can stop accepting work and restart instead of stalling.
func (w *Worker) Run(ctx context.Context) error {
	if w == nil {
		return ErrUnavailable
	}
	nextRecovery := time.Now().Add(w.recoveryInterval)
	var consecutiveErrors int
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if !time.Now().Before(nextRecovery) {
			w.recoverExpired(ctx)
			nextRecovery = time.Now().Add(w.recoveryInterval)
		}
		worked, err := w.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Per-job lease outcomes are not outages: another worker
			// reclaimed the row, so keep polling without counting them.
			if errors.Is(err, ErrLeaseConflict) || errors.Is(err, ErrNotFound) {
				consecutiveErrors = 0
			} else {
				consecutiveErrors++
				if consecutiveErrors > w.maxConsecutiveErrors {
					return fmt.Errorf("worker: %d consecutive store errors: %w", consecutiveErrors, err)
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(w.errorBackoff(consecutiveErrors)):
				}
				continue
			}
		} else {
			consecutiveErrors = 0
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

// recoverExpired runs one best-effort recovery sweep for the worker's claim
// kinds. Stores without recovery support are skipped, and sweep failures
// never fail the worker: the next interval retries, and a downed database
// surfaces through claim errors instead.
func (w *Worker) recoverExpired(ctx context.Context) {
	recoverer, ok := w.store.(leaseRecoverer)
	if !ok {
		return
	}
	_, _ = recoverer.RecoverExpiredLeasesForKinds(ctx, w.kinds...)
}

// errorBackoff spaces out retries after consecutiveErrors store failures:
// PollInterval doubling per failure, capped so a downed database neither
// hot-loops nor sleeps through a quick recovery.
func (w *Worker) errorBackoff(consecutiveErrors int) time.Duration {
	backoff := w.pollInterval
	if backoff <= 0 {
		backoff = time.Second
	}
	for attempt := 1; attempt < consecutiveErrors; attempt++ {
		backoff *= 2
		if backoff >= maxErrorBackoff {
			return maxErrorBackoff
		}
	}
	if backoff > maxErrorBackoff {
		return maxErrorBackoff
	}
	return backoff
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
	go w.renewLoop(handlerCtx, stopRenew, job.ID)
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

// renewLoop extends the row lease until handlerCtx ends. A renewal error
// means the lease is lost, so the loop cancels the handler context to stop
// its processing and returns; the handler's eventual Succeed/Fail then
// surfaces the conflict instead of completing a reclaimed row.
func (w *Worker) renewLoop(ctx context.Context, cancel context.CancelFunc, jobID string) {
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
			renewCtx, stopRenew := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := w.store.RenewLease(renewCtx, jobID, w.lockedBy, w.lease)
			stopRenew()
			if err != nil {
				cancel()
				return
			}
		}
	}
}
