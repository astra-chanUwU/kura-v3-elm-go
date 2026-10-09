package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store executes durable job transitions against PostgreSQL. It keeps the
// pool behind small interfaces used by the worker and the status endpoint.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps an existing pool. A nil pool keeps every method returning
// ErrUnavailable so the process can start before PostgreSQL is configured.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// NewStoreFromURL creates a store for databaseURL. The pool connects on
// demand, so callers can still construct the store while PostgreSQL is down.
func NewStoreFromURL(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close releases PostgreSQL connections owned by the store.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

const jobColumns = `
    id::text,
    kind,
    payload,
    status,
    idempotency_key,
    attempts,
    max_attempts,
    next_retry_at,
    last_attempt_at,
    locked_by,
    locked_at,
    lease_expires_at,
    progress,
    last_error,
    created_at,
    updated_at,
    completed_at
`

const enqueueInsertSQL = `
INSERT INTO jobs (kind, payload, status, idempotency_key, max_attempts)
VALUES ($1, $2::jsonb, 'pending', $3, $4)
ON CONFLICT DO NOTHING
RETURNING ` + jobColumns

const lookupByKindKeySQL = `
SELECT ` + jobColumns + `
FROM jobs
WHERE kind = $1 AND idempotency_key = $2
`

const getJobSQL = `
SELECT ` + jobColumns + `
FROM jobs
WHERE id = $1::bigint
`

// Enqueue inserts a pending row, or returns the existing row when the same
// (kind, idempotency key) was already enqueued. The two-step insert-then-
// select keeps the operation idempotent under concurrency: the unique
// constraint admits exactly one row per key.
func (s *Store) Enqueue(ctx context.Context, request EnqueueRequest) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	validated, err := ValidateEnqueue(request)
	if err != nil {
		return Job{}, err
	}
	var job Job
	if scanErr := s.scanJob(s.pool.QueryRow(ctx, enqueueInsertSQL,
		validated.Kind, string(validated.Payload), validated.IdempotencyKey, validated.MaxAttempts,
	), &job); scanErr == nil {
		return job, nil
	} else if !errors.Is(scanErr, pgx.ErrNoRows) {
		return Job{}, scanErr
	}
	if err := s.scanJob(s.pool.QueryRow(ctx, lookupByKindKeySQL,
		validated.Kind, validated.IdempotencyKey,
	), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	return job, nil
}

// EnqueueInTx inserts a pending row inside an existing transaction, or
// returns the existing row when the same (kind, idempotency key) was
// already enqueued. Upload creation uses it so the post row and its
// derivative job commit atomically: a rolled-back transaction leaves
// neither a post nor a queued job.
func EnqueueInTx(ctx context.Context, tx pgx.Tx, request EnqueueRequest) (Job, error) {
	validated, err := ValidateEnqueue(request)
	if err != nil {
		return Job{}, err
	}
	var job Job
	store := &Store{}
	if scanErr := store.scanJob(tx.QueryRow(ctx, enqueueInsertSQL,
		validated.Kind, string(validated.Payload), validated.IdempotencyKey, validated.MaxAttempts,
	), &job); scanErr == nil {
		return job, nil
	} else if !errors.Is(scanErr, pgx.ErrNoRows) {
		return Job{}, scanErr
	}
	if err := store.scanJob(tx.QueryRow(ctx, lookupByKindKeySQL,
		validated.Kind, validated.IdempotencyKey,
	), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	return job, nil
}

// RecoverExpiredLeases requeues crashed running rows and reports how many
// were recovered. Callers run it once at startup before workers claim, so
// interrupted work resumes instead of stalling on a dead lease. It
// delegates to the kind-scoped sweep with no filter so startup covers every
// kind; the worker's ongoing sweeps pass their claim kinds instead.
func (s *Store) RecoverExpiredLeases(ctx context.Context) (int64, error) {
	return s.RecoverExpiredLeasesForKinds(ctx)
}

// recoverExpiredForKindsSQL is the kind-scoped variant of recoverExpiredSQL:
// a nil or empty kinds slice recovers every kind, otherwise only rows whose
// kind matches. Backoff, attempt limits, and lease clearing match the
// startup sweep so stale owners stay rejected by Succeed and Fail.
const recoverExpiredForKindsSQL = `
UPDATE jobs
SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
    locked_by = NULL,
    locked_at = NULL,
    lease_expires_at = NULL,
    next_retry_at = CASE
        WHEN attempts >= max_attempts THEN NULL
        ELSE now() + (LEAST(600000, 5000 * POWER(2, GREATEST(attempts - 1, 0)))::bigint * interval '1 millisecond')
    END,
    last_error = CASE
        WHEN last_error = '' THEN 'lease expired without heartbeat'
        ELSE last_error
    END,
    completed_at = CASE WHEN attempts >= max_attempts THEN now() ELSE NULL END,
    updated_at = now()
WHERE status = 'running'
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at <= now()
  AND ($1::text[] IS NULL OR kind = ANY($1::text[]))
`

// RecoverExpiredLeasesForKinds requeues expired running rows restricted to
// kinds, reporting how many were recovered. The in-process worker calls it
// on a bounded interval with its own claim kinds (derivative in production)
// so leases that expire after startup are recovered without another
// restart; other kinds are never touched by that sweep.
func (s *Store) RecoverExpiredLeasesForKinds(ctx context.Context, kinds ...string) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, ErrUnavailable
	}
	var filter []string
	if len(kinds) > 0 {
		filter = append([]string(nil), kinds...)
	}
	tag, err := s.pool.Exec(ctx, recoverExpiredForKindsSQL, filter)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GetJob returns one job row for status polling.
func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	canonical, err := ParseID(id)
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := s.scanJob(s.pool.QueryRow(ctx, getJobSQL, canonical), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	return job, nil
}

const claimCandidateSQL = `
SELECT id
FROM jobs
WHERE status = 'pending'
  AND (next_retry_at IS NULL OR next_retry_at <= now())
  AND ($1::text[] IS NULL OR kind = ANY($1::text[]))
ORDER BY created_at ASC, id ASC
LIMIT 1
FOR UPDATE SKIP LOCKED
`

const claimUpdateSQL = `
UPDATE jobs
SET status = 'running',
    attempts = attempts + 1,
    locked_by = $2,
    locked_at = now(),
    lease_expires_at = now() + ($3::bigint * interval '1 millisecond'),
    last_attempt_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING ` + jobColumns

// Claim takes the oldest due pending row (optionally restricted to kinds)
// in a single transaction. SKIP LOCKED lets concurrent workers claim
// without blocking each other. It returns claimed=false when no row is due.
func (s *Store) Claim(ctx context.Context, lockedBy string, lease time.Duration, kinds ...string) (Job, bool, error) {
	if s == nil || s.pool == nil {
		return Job{}, false, ErrUnavailable
	}
	owner := strings.TrimSpace(lockedBy)
	if owner == "" {
		return Job{}, false, ErrInvalidJob
	}
	if lease <= 0 {
		return Job{}, false, ErrInvalidJob
	}
	var kindFilter []string
	if len(kinds) > 0 {
		kindFilter = append([]string(nil), kinds...)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var candidate int64
	if err := tx.QueryRow(ctx, claimCandidateSQL, kindFilter).Scan(&candidate); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	var job Job
	if err := s.scanJob(tx.QueryRow(ctx, claimUpdateSQL, candidate, owner, lease.Milliseconds()), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

const renewLeaseSQL = `
UPDATE jobs
SET lease_expires_at = now() + ($3::bigint * interval '1 millisecond'),
    updated_at = now()
WHERE id = $1::bigint
  AND locked_by = $2
  AND status = 'running'
  AND lease_expires_at > now()
RETURNING ` + jobColumns

// RenewLease extends the caller's running lease. It fails with
// ErrLeaseConflict when the row is missing, no longer running, owned by
// another worker, or already expired; callers must stop work then.
func (s *Store) RenewLease(ctx context.Context, id, lockedBy string, lease time.Duration) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	canonical, err := ParseID(id)
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(lockedBy) == "" || lease <= 0 {
		return Job{}, ErrInvalidJob
	}
	var job Job
	if err := s.scanJob(s.pool.QueryRow(ctx, renewLeaseSQL, canonical, lockedBy, lease.Milliseconds()), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.leaseFailure(ctx, canonical)
		}
		return Job{}, err
	}
	return job, nil
}

const progressSQL = `
UPDATE jobs
SET progress = $3,
    updated_at = now()
WHERE id = $1::bigint
  AND locked_by = $2
  AND status = 'running'
RETURNING ` + jobColumns

// UpdateProgress records 0-100 completion for the caller's running row.
func (s *Store) UpdateProgress(ctx context.Context, id, lockedBy string, progress int) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	canonical, err := ParseID(id)
	if err != nil {
		return Job{}, err
	}
	if err := ValidateProgress(progress); err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(lockedBy) == "" {
		return Job{}, ErrInvalidJob
	}
	var job Job
	if err := s.scanJob(s.pool.QueryRow(ctx, progressSQL, canonical, lockedBy, progress), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.leaseFailure(ctx, canonical)
		}
		return Job{}, err
	}
	return job, nil
}

const succeedSQL = `
UPDATE jobs
SET status = 'succeeded',
    progress = 100,
    locked_by = NULL,
    locked_at = NULL,
    lease_expires_at = NULL,
    next_retry_at = NULL,
    completed_at = now(),
    updated_at = now()
WHERE id = $1::bigint
  AND locked_by = $2
  AND status = 'running'
  AND lease_expires_at > now()
RETURNING ` + jobColumns

// Succeed marks the caller's running row succeeded and clears its lease.
// The row must still be running, owned by the caller, and inside its
// lease: a stale worker whose lease lapsed or was reclaimed gets
// ErrLeaseConflict instead of completing another worker's job.
func (s *Store) Succeed(ctx context.Context, id, lockedBy string) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	canonical, err := ParseID(id)
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(lockedBy) == "" {
		return Job{}, ErrInvalidJob
	}
	var job Job
	if err := s.scanJob(s.pool.QueryRow(ctx, succeedSQL, canonical, lockedBy), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.leaseFailure(ctx, canonical)
		}
		return Job{}, err
	}
	return job, nil
}

const failLockSQL = `
SELECT ` + jobColumns + `
FROM jobs
WHERE id = $1::bigint
FOR UPDATE
`

const failRetrySQL = `
UPDATE jobs
SET status = 'pending',
    locked_by = NULL,
    locked_at = NULL,
    lease_expires_at = NULL,
    next_retry_at = now() + ($2::bigint * interval '1 millisecond'),
    last_error = $3,
    updated_at = now()
WHERE id = $1::bigint
RETURNING ` + jobColumns

const failTerminalSQL = `
UPDATE jobs
SET status = 'failed',
    locked_by = NULL,
    locked_at = NULL,
    lease_expires_at = NULL,
    next_retry_at = NULL,
    last_error = $2,
    completed_at = now(),
    updated_at = now()
WHERE id = $1::bigint
RETURNING ` + jobColumns

// Fail records the caller's running row as failed work. While attempts
// remain the row returns to pending with next_retry_at set from RetryDelay;
// once attempts reach max_attempts the row is terminally failed. Both paths
// clear the lease and keep the error text for status polling.
func (s *Store) Fail(ctx context.Context, id, lockedBy, errMsg string) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	canonical, err := ParseID(id)
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(lockedBy) == "" {
		return Job{}, ErrInvalidJob
	}
	message := strings.TrimSpace(errMsg)
	if message == "" {
		message = "job failed"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current Job
	if err := s.scanJob(tx.QueryRow(ctx, failLockSQL, canonical), &current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	if current.Status != StatusRunning || current.LockedBy == nil || *current.LockedBy != lockedBy {
		return Job{}, ErrLeaseConflict
	}
	if current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(time.Now()) {
		return Job{}, ErrLeaseConflict
	}
	var job Job
	if current.Attempts < current.MaxAttempts {
		delay := RetryDelay(current.Attempts)
		if err := s.scanJob(tx.QueryRow(ctx, failRetrySQL, canonical, delay.Milliseconds(), message), &job); err != nil {
			return Job{}, err
		}
	} else {
		if err := s.scanJob(tx.QueryRow(ctx, failTerminalSQL, canonical, message), &job); err != nil {
			return Job{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	return job, nil
}

const cancelSQL = `
UPDATE jobs
SET status = 'cancelled',
    locked_by = NULL,
    locked_at = NULL,
    lease_expires_at = NULL,
    next_retry_at = NULL,
    completed_at = now(),
    updated_at = now()
WHERE id = $1::bigint
  AND status IN ('pending', 'running')
RETURNING ` + jobColumns

// Cancel moves a pending or running row to cancelled and clears its lease,
// releasing any holder. Terminal rows are returned unchanged so repeated
// cancellation is idempotent.
func (s *Store) Cancel(ctx context.Context, id string) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, ErrUnavailable
	}
	canonical, err := ParseID(id)
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := s.scanJob(s.pool.QueryRow(ctx, cancelSQL, canonical), &job); err == nil {
		return job, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Job{}, err
	}
	if err := s.scanJob(s.pool.QueryRow(ctx, getJobSQL, canonical), &job); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	return job, nil
}

// leaseFailure distinguishes a missing row from a lost lease after a
// conditional update matched nothing.
func (s *Store) leaseFailure(ctx context.Context, canonical string) error {
	var exists int
	if err := s.pool.QueryRow(ctx, `SELECT 1 FROM jobs WHERE id = $1::bigint`, canonical).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return ErrLeaseConflict
}

func (s *Store) scanJob(row pgx.Row, job *Job) error {
	var payload []byte
	if err := row.Scan(
		&job.ID,
		&job.Kind,
		&payload,
		&job.Status,
		&job.IdempotencyKey,
		&job.Attempts,
		&job.MaxAttempts,
		&job.NextRetryAt,
		&job.LastAttemptAt,
		&job.LockedBy,
		&job.LockedAt,
		&job.LeaseExpiresAt,
		&job.Progress,
		&job.LastError,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.CompletedAt,
	); err != nil {
		return err
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		payload = []byte("{}")
	}
	job.Payload = json.RawMessage(append([]byte(nil), payload...))
	return nil
}
