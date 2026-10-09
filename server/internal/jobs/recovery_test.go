package jobs

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDatabaseURL returns the disposable database URL for postgres-backed
// job tests, skipping when it is unset so unit runs stay green without a
// database. Never point it at development data: tests truncate jobs.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed job test")
	}
	return url
}

func newTestPool(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, filepath.Join("..", "..", "..", "db", "migrations", entry.Name()))
		}
	}
	sort.Strings(files)
	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	if _, err := pool.Exec(ctx, "TRUNCATE jobs"); err != nil {
		t.Fatal(err)
	}
	return pool
}

// TestRecoverExpiredLeases pins crash recovery: expired running rows with
// attempts left return to pending with backoff, exhausted rows fail
// terminally, and live leases plus pending rows are untouched.
func TestRecoverExpiredLeases(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t, ctx, testDatabaseURL(t))
	store := NewStore(pool)

	insert := func(kind, key string, attempts, maxAttempts int, status, lockedBy string, leaseAgo time.Duration) string {
		t.Helper()
		var lease any
		if leaseAgo != 0 {
			lease = time.Now().Add(leaseAgo)
		}
		var id string
		err := pool.QueryRow(ctx, `
INSERT INTO jobs (kind, payload, status, idempotency_key, attempts, max_attempts, locked_by, locked_at, lease_expires_at, last_attempt_at)
VALUES ($1, '{}', $2, $3, $4, $5, $6, now(), $7, now())
RETURNING id::text`,
			kind, status, key, attempts, maxAttempts, lockedBy, lease,
		).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	retryable := insert("derivative", "recover-retryable", 1, 5, StatusRunning, "dead-worker", -time.Minute)
	exhausted := insert("derivative", "recover-exhausted", 3, 3, StatusRunning, "dead-worker", -time.Minute)
	live := insert("derivative", "recover-live", 1, 5, StatusRunning, "busy-worker", time.Minute)
	pending := insert("derivative", "recover-pending", 0, 5, StatusPending, "", 0)

	recovered, err := store.RecoverExpiredLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Fatalf("recovered=%d want 2", recovered)
	}

	statusOf := func(id string) Job {
		t.Helper()
		job, err := store.GetJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return job
	}

	retried := statusOf(retryable)
	if retried.Status != StatusPending || retried.LockedBy != nil || retried.LeaseExpiresAt != nil {
		t.Fatalf("retryable not requeued: %#v", retried)
	}
	if retried.NextRetryAt == nil || retried.NextRetryAt.Before(time.Now()) {
		t.Fatalf("retryable missing backoff: %#v", retried.NextRetryAt)
	}
	if retried.LastError == "" {
		t.Fatal("retryable lost its lease-expiry marker")
	}

	failed := statusOf(exhausted)
	if failed.Status != StatusFailed || failed.CompletedAt == nil || failed.NextRetryAt != nil {
		t.Fatalf("exhausted attempts not terminal: %#v", failed)
	}

	if job := statusOf(live); job.Status != StatusRunning || job.LockedBy == nil || *job.LockedBy != "busy-worker" {
		t.Fatalf("live lease disturbed: %#v", job)
	}
	if job := statusOf(pending); job.Status != StatusPending {
		t.Fatalf("pending row disturbed: %#v", job)
	}
}

// TestEnqueueInTxIsIdempotentAndAtomic pins the upload transaction shape:
// one transaction carries the logical insert plus its job, retries of the
// same key collapse to one row, and a rolled-back transaction leaves no
// job behind.
func TestEnqueueInTxIsIdempotentAndAtomic(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t, ctx, testDatabaseURL(t))

	request, err := ValidateEnqueue(EnqueueRequest{Kind: "derivative", Payload: []byte(`{"post_id":"1"}`), IdempotencyKey: "tx-test:1:thumb-320"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := EnqueueInTx(ctx, tx, request)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	second, err := EnqueueInTx(ctx, tx, request)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if first.ID != second.ID {
		_ = tx.Rollback(ctx)
		t.Fatalf("retry duplicated logical job: %q vs %q", first.ID, second.ID)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(pool).GetJob(ctx, first.ID); err != nil {
		t.Fatalf("committed job missing: %v", err)
	}

	aborted, err := ValidateEnqueue(EnqueueRequest{Kind: "derivative", Payload: []byte(`{"post_id":"2"}`), IdempotencyKey: "tx-test:2:thumb-320"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueInTx(ctx, tx, aborted); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE idempotency_key = 'tx-test:2:thumb-320'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back transaction left %d job rows", count)
	}
}
