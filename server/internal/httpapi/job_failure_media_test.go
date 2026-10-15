package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

// disposableJobDatabase returns the disposable database URL, skipping when
// it is unset. Callers must point it at a throwaway database. The URL is
// never logged.
func disposableJobDatabase(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed job failure test")
	}
	return raw
}

func applyJobFailureMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
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
			t.Fatalf("apply migration: %v", err)
		}
	}
}

func generatedPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: 40, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func assertNoSecret(t *testing.T, label, body string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatalf("%s contains a storage path, credential, or stack trace", label)
		}
	}
}

// TestGeneratedMediaJobFailures proves the public status of real thumbnail
// and fingerprint jobs created from generated images: a missing file is
// retried with a sanitized error, an exhausted thumbnail stays terminally
// failed, a duplicate explicit fingerprint retry stays one row, and a
// request without write capability cannot reset that row.
func TestGeneratedMediaJobFailures(t *testing.T) {
	ctx := context.Background()
	databaseURL := disposableJobDatabase(t)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	applyJobFailureMigrations(t, ctx, pool)
	if _, err := pool.Exec(ctx, "TRUNCATE posts, jobs, asset_variants, tag_operations, tag_redirects CASCADE; UPDATE tag_taxonomy_state SET version=0 WHERE id=1"); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	backend, err := posts.NewPostgresBackendWithStore(ctx, databaseURL, media.NewLocalStore(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)

	const token = "kura-s6job-test-token"
	router := NewRouterWithToken(token, backend)
	poison := `C:\libraries\s6secretpath\photo.png password=s6hunter2 goroutine 3 [running]: derivative.go:141`

	t.Run("transient", func(t *testing.T) {
		detail := uploadGenerated(t, ctx, backend, "transient.png")
		key := mediaKey(t, detail.OriginalURL)
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(key))); err != nil {
			t.Fatal(err)
		}
		if !runDerivative(t, backend) {
			t.Fatal("derivative worker claimed no job")
		}
		stored, err := backend.GetJob(ctx, detail.DerivativeJobID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != jobs.StatusPending || stored.Attempts != 1 || stored.NextRetryAt == nil || stored.LastError != "The file could not be read." {
			t.Fatalf("transient job status=%s attempts=%d retry=%v", stored.Status, stored.Attempts, stored.NextRetryAt != nil)
		}
		assertNoSecret(t, "stored error", stored.LastError, key, detail.Hash, root, "s6secretpath")
		body := getJobBody(t, router, detail.DerivativeJobID)
		assertNoSecret(t, "public status", body, key, detail.Hash, root, "s6secretpath", "original_key", "payload")
		public := decodePublic(t, body)
		if public.Action != jobs.ActionWait || public.LastError != "The file could not be read." || public.Attempts != 1 || public.NextRetryAt == nil {
			t.Fatalf("transient public action=%s attempts=%d", public.Action, public.Attempts)
		}
		if _, err := pool.Exec(ctx, "UPDATE jobs SET next_retry_at = now() + interval '1 hour' WHERE id = $1::bigint", detail.DerivativeJobID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("permanent", func(t *testing.T) {
		detail := uploadGenerated(t, ctx, backend, "permanent.png")
		key := mediaKey(t, detail.OriginalURL)
		if _, err := pool.Exec(ctx, "UPDATE jobs SET max_attempts = 1 WHERE id = $1::bigint", detail.DerivativeJobID); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(key))); err != nil {
			t.Fatal(err)
		}
		if !runDerivative(t, backend) {
			t.Fatal("derivative worker claimed no job")
		}
		stored, err := backend.GetJob(ctx, detail.DerivativeJobID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != jobs.StatusFailed || stored.Attempts != 1 || stored.NextRetryAt != nil || stored.CompletedAt == nil {
			t.Fatalf("permanent job status=%s attempts=%d", stored.Status, stored.Attempts)
		}
		assertNoSecret(t, "stored error", stored.LastError, key, detail.Hash, root)
		body := getJobBody(t, router, detail.DerivativeJobID)
		assertNoSecret(t, "public status", body, key, detail.Hash, root, "original_key", "payload")
		public := decodePublic(t, body)
		if public.Action != jobs.ActionThumbnailFailed || public.LastError != "The file could not be read." || public.Status != jobs.StatusFailed {
			t.Fatalf("permanent public action=%s status=%s", public.Action, public.Status)
		}
		if runDerivative(t, backend) {
			t.Fatal("terminally failed thumbnail was claimed again")
		}
	})

	t.Run("duplicate retry and unauthorized", func(t *testing.T) {
		detail := uploadGenerated(t, ctx, backend, "retry.png")
		key := mediaKey(t, detail.OriginalURL)
		var jobID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM jobs WHERE kind = 'fingerprint' AND payload->>'post_id' = $1`, detail.ID).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE jobs SET status = 'failed', attempts = max_attempts,
			next_retry_at = NULL, locked_by = NULL, locked_at = NULL, lease_expires_at = NULL,
			last_error = $2, completed_at = now(), updated_at = now() WHERE id = $1::bigint`, jobID, poison); err != nil {
			t.Fatal(err)
		}
		body := getJobBody(t, router, jobID)
		assertNoSecret(t, "public fingerprint status", body, key, detail.Hash, "s6secretpath", "s6hunter2", "goroutine", "password", ".go:")
		before := decodePublic(t, body)
		if before.Action != jobs.ActionIndexPost || before.Status != jobs.StatusFailed || before.LastError != "The job failed." {
			t.Fatalf("failed fingerprint action=%s status=%s", before.Action, before.Status)
		}

		for _, header := range []string{"", "Bearer not-the-token"} {
			response := postIndex(router, header, detail.ID)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("unauthorized index status=%d", response.Code)
			}
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1::bigint`, jobID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != jobs.StatusFailed {
			t.Fatalf("unauthorized retry changed job to %s", status)
		}

		const callers = 4
		codes := make([]int, callers)
		raws := make([][]byte, callers)
		var ready sync.WaitGroup
		for i := 0; i < callers; i++ {
			ready.Add(1)
			go func(index int) {
				defer ready.Done()
				response := postIndex(router, "Bearer "+token, detail.ID)
				codes[index] = response.Code
				raws[index] = append([]byte(nil), response.Body.Bytes()...)
			}(i)
		}
		ready.Wait()
		for i := range codes {
			decoded := decodeIndex(t, raws[i])
			if codes[i] != http.StatusOK || len(decoded.Posts) != 1 || decoded.Posts[0].JobID != jobID || decoded.Posts[0].Status != jobs.StatusPending {
				t.Fatalf("caller %d status=%d posts=%d", i, codes[i], len(decoded.Posts))
			}
		}
		followUp := postIndex(router, "Bearer "+token, detail.ID)
		if followUp.Code != http.StatusOK {
			t.Fatalf("follow-up retry status=%d", followUp.Code)
		}
		again := decodeIndex(t, followUp.Body.Bytes())
		if len(again.Posts) != 1 || again.Posts[0].JobID != jobID || again.Posts[0].Status != jobs.StatusPending {
			t.Fatal("follow-up retry did not return the same pending job")
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'fingerprint' AND payload->>'post_id' = $1`, detail.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("duplicate retry created %d fingerprint jobs", count)
		}
		after := decodePublic(t, getJobBody(t, router, jobID))
		if after.Status != jobs.StatusPending || after.Action != jobs.ActionNone || after.Attempts != 0 || after.LastError != "" {
			t.Fatalf("retried fingerprint status=%s action=%s attempts=%d", after.Status, after.Action, after.Attempts)
		}
	})
}

func uploadGenerated(t *testing.T, ctx context.Context, backend *posts.PostgresBackend, name string) posts.PostDetail {
	t.Helper()
	detail, err := backend.CreateUpload(ctx, posts.UploadRequest{
		File:     bytes.NewReader(generatedPNG(t)),
		Filename: name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.DerivativeJobID == "" || detail.ID == "" {
		t.Fatal("upload did not return a post and derivative job")
	}
	return detail
}

func mediaKey(t *testing.T, originalURL string) string {
	t.Helper()
	key := strings.TrimPrefix(originalURL, "/media/")
	if key == "" || key == originalURL || strings.Contains(key, "..") {
		t.Fatal("original URL is not a local media key")
	}
	return key
}

func runDerivative(t *testing.T, backend *posts.PostgresBackend) bool {
	t.Helper()
	worker, err := jobs.NewWorker(jobs.WorkerConfig{
		Store:        backend.JobsStore(),
		Handler:      backend.NewDerivativeProcessor().Handle,
		LockedBy:     "s6-job-test",
		Kinds:        []string{media.KindDerivative},
		Lease:        time.Second,
		PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return worked
}

func getJobBody(t *testing.T, router http.Handler, id string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/jobs/"+id, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("job status=%d", response.Code)
	}
	return response.Body.String()
}

func decodePublic(t *testing.T, body string) jobs.PublicJob {
	t.Helper()
	var public jobs.PublicJob
	if err := json.Unmarshal([]byte(body), &public); err != nil {
		t.Fatal(err)
	}
	return public
}

type indexResult struct {
	Posts []struct {
		ID     string `json:"id"`
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	} `json:"posts"`
}

func postIndex(router http.Handler, authorization, postID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/posts/similarity/index", strings.NewReader(`{"post_ids":["`+postID+`"]}`))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeIndex(t *testing.T, raw []byte) indexResult {
	t.Helper()
	var result indexResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
