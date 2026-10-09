package posts

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// uploadTestDatabaseURL returns the disposable database URL for
// postgres-backed upload tests, skipping when unset so unit runs stay
// green without a database. Never point it at development data.
func uploadTestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed upload test")
	}
	return url
}

func newUploadTestSearcher(t *testing.T, ctx context.Context, url, root string) *PostgresSearcher {
	t.Helper()
	searcher, err := NewPostgresSearcherWithStore(ctx, url, media.NewLocalStore(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(searcher.Close)
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
		if _, err := searcher.pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	if _, err := searcher.pool.Exec(ctx, "TRUNCATE posts, jobs, asset_variants CASCADE"); err != nil {
		t.Fatal(err)
	}
	return searcher
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func runDerivativeOnce(t *testing.T, searcher *PostgresSearcher) bool {
	t.Helper()
	worker, err := jobs.NewWorker(jobs.WorkerConfig{
		Store:    searcher.JobsStore(),
		Handler:  searcher.NewDerivativeProcessor().Handle,
		LockedBy: "upload-test-worker",
		Kinds:    []string{media.KindDerivative},
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

// TestCreateUploadEnqueuesDerivativeJob pins the atomic upload slice: one
// transaction creates the post and its default thumbnail job, the detail
// exposes the job for polling, the original serves as preview while
// processing, and the ready thumbnail takes over without touching the
// original URL or hash.
func TestCreateUploadEnqueuesDerivativeJob(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	searcher := newUploadTestSearcher(t, ctx, uploadTestDatabaseURL(t), root)

	detail, err := searcher.CreateUpload(ctx, UploadRequest{
		File:     bytes.NewReader(testPNG(t, 640, 480)),
		Filename: "photo.png",
		Tags:     []string{"demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.DerivativeJobID == "" {
		t.Fatal("upload did not expose its derivative job id")
	}
	if detail.DerivativeStatus != jobs.StatusPending {
		t.Fatalf("derivative status=%q want %q", detail.DerivativeStatus, jobs.StatusPending)
	}
	if detail.PreviewURL != detail.OriginalURL {
		t.Fatalf("processing preview=%q should serve the original %q", detail.PreviewURL, detail.OriginalURL)
	}
	originalURL, hash := detail.OriginalURL, detail.Hash

	job, err := searcher.GetJob(ctx, detail.DerivativeJobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("job payload not JSON: %v", err)
	}
	if job.Kind != media.KindDerivative || payload["post_id"] != detail.ID || payload["variant"] != media.DefaultDerivativeVariant || payload["original_key"] == "" {
		t.Fatalf("job not bound to upload: %#v payload=%v", job, payload)
	}

	if !runDerivativeOnce(t, searcher) {
		t.Fatal("derivative worker claimed no job")
	}
	if job, err = searcher.GetJob(ctx, detail.DerivativeJobID); err != nil || job.Status != jobs.StatusSucceeded {
		t.Fatalf("job after work: %#v %v", job, err)
	}

	ready, err := searcher.GetPostDetail(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.OriginalURL != originalURL || ready.Hash != hash {
		t.Fatalf("original drifted: url %q hash %q", ready.OriginalURL, ready.Hash)
	}
	if !strings.HasPrefix(ready.PreviewURL, "/media/derivatives/thumb-320/") {
		t.Fatalf("ready preview=%q", ready.PreviewURL)
	}
	if ready.DerivativeStatus != jobs.StatusSucceeded {
		t.Fatalf("ready derivative status=%q", ready.DerivativeStatus)
	}
	thumbnail, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(ready.PreviewURL, "/media/"))))
	if err != nil {
		t.Fatalf("thumbnail missing on disk: %v", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("thumbnail not decodable: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("thumbnail format=%q want jpeg", format)
	}
	if longEdge := max(config.Width, config.Height); longEdge > 320 {
		t.Fatalf("thumbnail long edge=%d exceeds 320px", longEdge)
	}
	if config.Width != 320 || config.Height != 240 {
		t.Fatalf("thumbnail dimensions=%dx%d want 320x240", config.Width, config.Height)
	}

	// Retrying the same logical job enqueues no duplicate and records no
	// second variant row.
	second, err := searcher.JobsStore().Enqueue(ctx, mustDerivativeRequest(t, detail.ID, originalKeyForURL(t, originalURL)))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != detail.DerivativeJobID {
		t.Fatalf("retry duplicated logical job: %q vs %q", second.ID, detail.DerivativeJobID)
	}
	var variants int
	if err := searcher.pool.QueryRow(ctx, `SELECT count(*) FROM asset_variants WHERE post_id = $1::bigint`, detail.ID).Scan(&variants); err != nil {
		t.Fatal(err)
	}
	if variants != 1 {
		t.Fatalf("variants=%d want 1", variants)
	}
}

// TestUploadTransactionFailureLeavesNoRows pins atomicity from the failure
// side: the post insert and its job enqueue share one transaction, so an
// aborted transaction leaves neither row behind.
func TestUploadTransactionFailureLeavesNoRows(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	searcher := newUploadTestSearcher(t, ctx, uploadTestDatabaseURL(t), root)

	tx, err := searcher.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := tx.QueryRow(ctx, insertUploadedPostSQL,
		"/media/uploads/aborted.png", "/media/uploads/aborted.png", "image/png",
		10, 10, "aborted", "aborted", "", "sha256:aborted", 100, []string{"aborted"},
	).Scan(&id); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	request, err := media.EnqueueDerivative(id, "uploads/aborted.png", media.DefaultDerivativeVariant)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err := jobs.EnqueueInTx(ctx, tx, request); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var posts, queued int
	if err := searcher.pool.QueryRow(ctx, `SELECT count(*) FROM posts WHERE id = $1::bigint`, id).Scan(&posts); err != nil {
		t.Fatal(err)
	}
	if err := searcher.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE idempotency_key = 'derivative:`+id+`:thumb-320'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if posts != 0 || queued != 0 {
		t.Fatalf("aborted transaction left posts=%d jobs=%d", posts, queued)
	}
}

// TestDerivativeMissingOriginalRecordsRetry pins failure reporting: work on
// a lost original fails the attempt with an error instead of succeeding,
// leaving the job retryable and the original preview in place.
func TestDerivativeMissingOriginalRecordsRetry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	searcher := newUploadTestSearcher(t, ctx, uploadTestDatabaseURL(t), root)

	detail, err := searcher.CreateUpload(ctx, UploadRequest{
		File:     bytes.NewReader(testPNG(t, 200, 200)),
		Filename: "vanishing.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(originalKeyForURL(t, detail.OriginalURL)))); err != nil {
		t.Fatal(err)
	}
	if !runDerivativeOnce(t, searcher) {
		t.Fatal("derivative worker claimed no job")
	}
	job, err := searcher.GetJob(ctx, detail.DerivativeJobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 1 || job.Status != jobs.StatusPending {
		t.Fatalf("missing original not retryable: %#v", job)
	}
	if !strings.Contains(job.LastError, "open original") {
		t.Fatalf("missing original error not recorded: %q", job.LastError)
	}
}

// TestHiddenPostDeniesOriginalAndThumbnail pins moderation gating for every
// derivative URL: hiding the post denies both the original and the ready
// thumbnail, and republishing restores both.
func TestHiddenPostDeniesOriginalAndThumbnail(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	searcher := newUploadTestSearcher(t, ctx, uploadTestDatabaseURL(t), root)

	detail, err := searcher.CreateUpload(ctx, UploadRequest{
		File:     bytes.NewReader(testPNG(t, 640, 480)),
		Filename: "gated.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runDerivativeOnce(t, searcher) {
		t.Fatal("derivative worker claimed no job")
	}
	ready, err := searcher.GetPostDetail(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	thumbnailURL := ready.PreviewURL
	if !strings.HasPrefix(thumbnailURL, "/media/derivatives/") {
		t.Fatalf("thumbnail not ready: %q", thumbnailURL)
	}

	visible := func(url string) bool {
		visibility, err := searcher.LookupMediaVisibility(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		return visibility.Referenced && visibility.Visible
	}
	if !visible(ready.OriginalURL) || !visible(thumbnailURL) {
		t.Fatal("published media should be visible")
	}
	if _, err := searcher.ModeratePosts(ctx, "test", ModerationRequest{
		Posts:  []ModerationTarget{{ID: detail.ID}},
		Action: ModerationActionHide,
	}); err != nil {
		t.Fatal(err)
	}
	if visible(ready.OriginalURL) || visible(thumbnailURL) {
		t.Fatal("hidden original or thumbnail still visible")
	}
	if _, err := searcher.GetPostDetail(ctx, detail.ID); err == nil {
		t.Fatal("hidden post detail should be indistinguishable from missing")
	}
	if _, err := searcher.ModeratePosts(ctx, "test", ModerationRequest{
		Posts:  []ModerationTarget{{ID: detail.ID}},
		Action: ModerationActionApprove,
	}); err != nil {
		t.Fatal(err)
	}
	if !visible(ready.OriginalURL) || !visible(thumbnailURL) {
		t.Fatal("republished media should be visible again")
	}
	restored, err := searcher.GetPostDetail(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.PreviewURL != thumbnailURL || restored.OriginalURL != ready.OriginalURL {
		t.Fatalf("republish changed media URLs: %#v", restored)
	}
}

func mustDerivativeRequest(t *testing.T, postID, originalKey string) jobs.EnqueueRequest {
	t.Helper()
	request, err := media.EnqueueDerivative(postID, originalKey, media.DefaultDerivativeVariant)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func originalKeyForURL(t *testing.T, url string) string {
	t.Helper()
	key := strings.TrimPrefix(url, "/media/")
	if key == "" || key == url {
		t.Fatalf("unexpected media URL %q", url)
	}
	return key
}
