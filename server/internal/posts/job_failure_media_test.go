package posts

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
)

func assertJobTextHasNoSecret(t *testing.T, label, body string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatalf("%s contains a storage path, credential, or stack trace", label)
		}
	}
}

// TestGeneratedMediaJobFailureStatus proves thumbnail and fingerprint rows
// created from generated images. A missing original is retried with a
// sanitized error, exhausting the attempt budget fails the thumbnail for
// good, and concurrent explicit index calls reset one fingerprint row once.
func TestGeneratedMediaJobFailureStatus(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := newUploadTestBackend(t, ctx, uploadTestDatabaseURL(t), root)
	poison := `C:\libraries\s6secretpath\photo.png password=s6hunter2 goroutine 3 [running]: derivative.go:141`

	t.Run("transient", func(t *testing.T) {
		detail, err := backend.CreateUpload(ctx, UploadRequest{
			File:     bytes.NewReader(testPNG(t, 32, 24)),
			Filename: "transient.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		key := originalKeyForURL(t, detail.OriginalURL)
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(key))); err != nil {
			t.Fatal(err)
		}
		if !runDerivativeOnce(t, backend) {
			t.Fatal("derivative worker claimed no job")
		}
		stored, err := backend.GetJob(ctx, detail.DerivativeJobID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != jobs.StatusPending || stored.Attempts != 1 || stored.NextRetryAt == nil || stored.LastError != "The file could not be read." {
			t.Fatalf("transient job status=%s attempts=%d retry=%v", stored.Status, stored.Attempts, stored.NextRetryAt != nil)
		}
		public := stored.Public()
		encoded, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		assertJobTextHasNoSecret(t, "public status", string(encoded), key, detail.Hash, root, "original_key", "payload", "s6secretpath")
		if public.Action != jobs.ActionWait || public.LastError != "The file could not be read." {
			t.Fatalf("transient public action=%s", public.Action)
		}
		if _, err := backend.pool.Exec(ctx, "UPDATE jobs SET next_retry_at = now() + interval '1 hour' WHERE id = $1::bigint", detail.DerivativeJobID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("permanent", func(t *testing.T) {
		detail, err := backend.CreateUpload(ctx, UploadRequest{
			File:     bytes.NewReader(testPNG(t, 32, 24)),
			Filename: "permanent.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		key := originalKeyForURL(t, detail.OriginalURL)
		if _, err := backend.pool.Exec(ctx, "UPDATE jobs SET max_attempts = 1 WHERE id = $1::bigint", detail.DerivativeJobID); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(key))); err != nil {
			t.Fatal(err)
		}
		if !runDerivativeOnce(t, backend) {
			t.Fatal("derivative worker claimed no job")
		}
		stored, err := backend.GetJob(ctx, detail.DerivativeJobID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != jobs.StatusFailed || stored.Attempts != 1 || stored.NextRetryAt != nil || stored.CompletedAt == nil || stored.LastError != "The file could not be read." {
			t.Fatalf("permanent job status=%s attempts=%d", stored.Status, stored.Attempts)
		}
		public := stored.Public()
		encoded, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		assertJobTextHasNoSecret(t, "public status", string(encoded), key, detail.Hash, root, "original_key", "payload")
		if public.Action != jobs.ActionOriginalSaved || public.Status != jobs.StatusFailed {
			t.Fatalf("permanent public action=%s status=%s", public.Action, public.Status)
		}
		if runDerivativeOnce(t, backend) {
			t.Fatal("terminally failed thumbnail was claimed again")
		}
	})

	t.Run("duplicate explicit retry", func(t *testing.T) {
		detail, err := backend.CreateUpload(ctx, UploadRequest{
			File:     bytes.NewReader(testPNG(t, 32, 24)),
			Filename: "retry.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		key := originalKeyForURL(t, detail.OriginalURL)
		var jobID string
		if err := backend.pool.QueryRow(ctx, `SELECT id::text FROM jobs WHERE kind = 'fingerprint' AND payload->>'post_id' = $1`, detail.ID).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.pool.Exec(ctx, `UPDATE jobs SET status = 'failed', attempts = max_attempts,
			next_retry_at = NULL, locked_by = NULL, locked_at = NULL, lease_expires_at = NULL,
			last_error = $2, completed_at = now(), updated_at = now() WHERE id = $1::bigint`, jobID, poison); err != nil {
			t.Fatal(err)
		}
		stored, err := backend.GetJob(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		public := stored.Public()
		encoded, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		assertJobTextHasNoSecret(t, "public fingerprint status", string(encoded), key, detail.Hash, "s6secretpath", "s6hunter2", "goroutine", "password", ".go:")
		if public.Action != jobs.ActionIndexPost || public.Status != jobs.StatusFailed || public.LastError != "The job failed." {
			t.Fatalf("failed fingerprint action=%s status=%s", public.Action, public.Status)
		}

		const callers = 4
		results := make([]SimilarityIndexResponse, callers)
		errs := make([]error, callers)
		var ready sync.WaitGroup
		for i := 0; i < callers; i++ {
			ready.Add(1)
			go func(index int) {
				defer ready.Done()
				results[index], errs[index] = backend.IndexSimilarityPosts(ctx, []string{detail.ID})
			}(i)
		}
		ready.Wait()
		for i := range results {
			if errs[i] != nil || len(results[i].Posts) != 1 || results[i].Posts[0].JobID != jobID || results[i].Posts[0].Status != jobs.StatusPending {
				t.Fatalf("caller %d failed", i)
			}
		}
		again, err := backend.IndexSimilarityPosts(ctx, []string{detail.ID})
		if err != nil || len(again.Posts) != 1 || again.Posts[0].JobID != jobID || again.Posts[0].Status != jobs.StatusPending {
			t.Fatal("follow-up retry did not return the same pending job")
		}
		var count int
		if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'fingerprint' AND payload->>'post_id' = $1`, detail.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("duplicate retry created %d fingerprint jobs", count)
		}
		retried, err := backend.GetJob(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		after := retried.Public()
		if after.Status != jobs.StatusPending || after.Action != jobs.ActionNone || after.Attempts != 0 || after.LastError != "" {
			t.Fatalf("retried fingerprint status=%s action=%s attempts=%d", after.Status, after.Action, after.Attempts)
		}
	})
}
