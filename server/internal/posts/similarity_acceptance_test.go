package posts

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// Index controls and status pages must use the same eligibility rule. A
// published video remains visible to ordinary library browsing, but must
// never enter the image fingerprint queue.
func TestSimilarityIndexRejectsNonImagePosts(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	post := uploadSimilarityFixture(t, backend, ctx, 0, "reclassified.jpg")

	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET media_type = 'video/mp4' WHERE id = $1::bigint`, post.ID); err != nil {
		t.Fatalf("reclassify fixture as video: %v", err)
	}

	if _, err := backend.IndexSimilarityPosts(ctx, []string{post.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("index non-image error=%v, want ErrNotFound", err)
	}
	status, err := backend.GetSimilarityStatus(ctx, 100, "")
	if err != nil {
		t.Fatalf("GetSimilarityStatus: %v", err)
	}
	if status.EligibleCount != 0 || len(status.Posts) != 0 {
		t.Fatalf("non-image appeared in image index status: %+v", status)
	}
}

func setSimilarityJobState(t *testing.T, backend *PostgresBackend, jobID, state string) {
	t.Helper()
	var query string
	switch state {
	case "failed":
		query = `UPDATE jobs SET status = 'failed', attempts = max_attempts,
			next_retry_at = NULL, locked_by = NULL, locked_at = NULL,
			lease_expires_at = NULL, last_error = 'acceptance fixture failure',
			completed_at = now(), updated_at = now() WHERE id = $1::bigint`
	case "running":
		query = `UPDATE jobs SET status = 'running', attempts = 1,
			next_retry_at = NULL, locked_by = 'acceptance-fixture', locked_at = now(),
			lease_expires_at = now() + interval '1 minute', last_error = '',
			completed_at = NULL, updated_at = now() WHERE id = $1::bigint`
	default:
		t.Fatalf("unsupported fixture state %q", state)
	}
	result, err := backend.pool.Exec(context.Background(), query, jobID)
	if err != nil {
		t.Fatalf("set job %s state to %s: %v", jobID, state, err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("set job %s state to %s affected %d rows", jobID, state, result.RowsAffected())
	}
}

func TestSimilarityStatusDoesNotRetryFailedFingerprintJob(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	post := uploadSimilarityFixture(t, backend, ctx, 0, "failed.jpg")
	indexed, err := backend.IndexSimilarityPosts(ctx, []string{post.ID})
	if err != nil || len(indexed.Posts) != 1 {
		t.Fatalf("IndexSimilarityPosts=%+v err=%v", indexed, err)
	}
	jobID := indexed.Posts[0].JobID
	setSimilarityJobState(t, backend, jobID, "failed")

	for i := 0; i < 2; i++ {
		status, err := backend.GetSimilarityStatus(ctx, 100, "")
		if err != nil {
			t.Fatalf("GetSimilarityStatus: %v", err)
		}
		if len(status.Posts) != 1 || status.Posts[0].JobID == nil || *status.Posts[0].JobID != jobID || status.Posts[0].Status == nil || *status.Posts[0].Status != "failed" {
			t.Fatalf("status read changed or hid terminal failure: %+v", status)
		}
	}
	var jobStatus string
	if err := backend.pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1::bigint`, jobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" {
		t.Fatalf("read-only status changed job to %q", jobStatus)
	}
}

func TestSimilarityExplicitIndexRetriesTerminalFailure(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	post := uploadSimilarityFixture(t, backend, ctx, 0, "retry.jpg")
	first, err := backend.IndexSimilarityPosts(ctx, []string{post.ID})
	if err != nil || len(first.Posts) != 1 {
		t.Fatalf("initial IndexSimilarityPosts=%+v err=%v", first, err)
	}
	jobID := first.Posts[0].JobID
	setSimilarityJobState(t, backend, jobID, "failed")

	retried, err := backend.IndexSimilarityPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("explicit retry IndexSimilarityPosts: %v", err)
	}
	if len(retried.Posts) != 1 || retried.Posts[0].JobID != jobID || retried.Posts[0].Status != "pending" {
		t.Fatalf("explicit retry=%+v, want same job id pending", retried)
	}
}

func TestSimilarityConcurrentExplicitRetryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	post := uploadSimilarityFixture(t, backend, ctx, 0, "concurrent-retry.jpg")
	first, err := backend.IndexSimilarityPosts(ctx, []string{post.ID})
	if err != nil || len(first.Posts) != 1 {
		t.Fatalf("initial IndexSimilarityPosts=%+v err=%v", first, err)
	}
	jobID := first.Posts[0].JobID
	setSimilarityJobState(t, backend, jobID, "failed")

	const callers = 4
	results := make([]SimilarityIndexResponse, callers)
	errs := make([]error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index], errs[index] = backend.IndexSimilarityPosts(ctx, []string{post.ID})
		}(i)
	}
	wait.Wait()
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if len(results[i].Posts) != 1 || results[i].Posts[0].JobID != jobID || results[i].Posts[0].Status != "pending" {
			t.Fatalf("caller %d result=%+v, want same job id pending", i, results[i])
		}
	}
	var count int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'fingerprint'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent retry created %d fingerprint jobs, want 1", count)
	}
}

func TestSimilarityExplicitIndexPreservesRunningFingerprintJob(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	post := uploadSimilarityFixture(t, backend, ctx, 0, "running.jpg")
	first, err := backend.IndexSimilarityPosts(ctx, []string{post.ID})
	if err != nil || len(first.Posts) != 1 {
		t.Fatalf("initial IndexSimilarityPosts=%+v err=%v", first, err)
	}
	jobID := first.Posts[0].JobID
	setSimilarityJobState(t, backend, jobID, "running")

	again, err := backend.IndexSimilarityPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("IndexSimilarityPosts with running job: %v", err)
	}
	if len(again.Posts) != 1 || again.Posts[0].JobID != jobID || again.Posts[0].Status != "running" {
		t.Fatalf("running job was changed: %+v", again)
	}
}
