package posts

// Postgres-backed similarity coverage. Every test here skips without
// KURA_TEST_DATABASE_URL so unit runs stay green without a database; the
// coordinator runs them against disposable databases. Never point the URL
// at development data.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// similarityFixture renders the same low-frequency scene as the media
// package's unit fixture so DB assertions inherit its calibrated
// separation: resized/recompressed copies land within distance 8 while a
// mirrored scene lands far beyond radius 16.
func similarityFixture(t *testing.T, variant, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var value int
			if variant == 0 {
				value = x*200/width + y*40/height
			} else {
				value = (width-x)*200/width + (height-y)*40/height
			}
			picture.Set(x, y, color.RGBA{R: uint8(value % 256), G: uint8((value * 3 / 4) % 256), B: uint8((100 + value/3) % 256), A: 255})
		}
	}
	disc := func(cx, cy, radius int, c color.RGBA) {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy < radius*radius {
					picture.Set(x, y, c)
				}
			}
		}
	}
	if variant == 0 {
		disc(width/4, height/2, height/5, color.RGBA{15, 15, 15, 255})
		for x := 0; x < width; x++ {
			for y := height * 4 / 5; y < height; y++ {
				picture.Set(x, y, color.RGBA{235, 235, 235, 255})
			}
		}
	} else {
		disc(3*width/4, height/2, height/5, color.RGBA{235, 235, 235, 255})
		for x := 0; x < width; x++ {
			for y := 0; y < height/5; y++ {
				picture.Set(x, y, color.RGBA{15, 15, 15, 255})
			}
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return encoded.Bytes()
}

func uploadSimilarityFixture(t *testing.T, backend *PostgresBackend, ctx context.Context, variant int, name string) PostDetail {
	t.Helper()
	detail, err := backend.CreateUpload(ctx, UploadRequest{
		File:     bytes.NewReader(similarityFixture(t, variant, 96, 72)),
		Filename: name,
	})
	if err != nil {
		t.Fatalf("CreateUpload %s: %v", name, err)
	}
	return detail
}

// runFingerprintJobs claims fingerprint rows through the shared media
// dispatcher until none remain, proving HandleMediaJob serves the new
// kind while the derivative worker path stays untouched.
func runFingerprintJobs(t *testing.T, backend *PostgresBackend) int {
	t.Helper()
	worker, err := jobs.NewWorker(jobs.WorkerConfig{
		Store:    backend.JobsStore(),
		Handler:  backend.HandleMediaJob,
		LockedBy: "similarity-test-worker",
		Kinds:    []string{media.KindFingerprint},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked := 0
	for i := 0; i < 20; i++ {
		claimed, err := worker.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !claimed {
			break
		}
		worked++
	}
	return worked
}

func newSimilarityBackend(t *testing.T, ctx context.Context) (*PostgresBackend, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	return newUploadTestBackend(t, ctx, uploadTestDatabaseURL(t), root), root
}

func TestSimilarityUploadEnqueuesFingerprintAtomically(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	detail, err := backend.CreateUpload(ctx, UploadRequest{
		File:     bytes.NewReader(similarityFixture(t, 0, 96, 72)),
		Filename: "indexed.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'fingerprint'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fingerprint jobs=%d want 1 alongside the derivative job", count)
	}
	var key, payloadHash, postID string
	if err := backend.pool.QueryRow(ctx, `SELECT idempotency_key, payload->>'post_id', payload->>'original_hash' FROM jobs WHERE kind = 'fingerprint'`).Scan(&key, &postID, &payloadHash); err != nil {
		t.Fatal(err)
	}
	if want := media.FingerprintIdempotencyKey(mustParseID(t, detail.ID), detail.Hash); key != want {
		t.Fatalf("idempotency key=%q want %q", key, want)
	}
	if postID != detail.ID || payloadHash != detail.Hash {
		t.Fatalf("payload not bound: post %q hash %q", postID, payloadHash)
	}
	status, err := backend.GetSimilarityStatus(ctx, 100, "")
	if err != nil {
		t.Fatalf("GetSimilarityStatus: %v", err)
	}
	if status.EligibleCount != 1 || status.IndexedCount != 0 {
		t.Fatalf("before work status=%+v", status)
	}
	if len(status.Posts) != 1 || status.Posts[0].ID != detail.ID || status.Posts[0].Indexed {
		t.Fatalf("before work page=%+v", status)
	}
	if status.Posts[0].JobID == nil || status.Posts[0].Status == nil {
		t.Fatal("pending job must be visible to the index tool")
	}
}

func mustParseID(t *testing.T, raw string) int64 {
	t.Helper()
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		t.Fatalf("bad test post id %q: %v", raw, err)
	}
	return id
}

func TestSimilarityNearCopiesMatchEndToEnd(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	anchor := uploadSimilarityFixture(t, backend, ctx, 0, "anchor.jpg")
	near := uploadSimilarityFixture(t, backend, ctx, 0, "near.jpg")
	far := uploadSimilarityFixture(t, backend, ctx, 1, "far.jpg")
	if runFingerprintJobs(t, backend) != 3 {
		t.Fatal("expected three fingerprint jobs to run")
	}
	page, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 30, "")
	if err != nil {
		t.Fatalf("ListSimilarPosts: %v", err)
	}
	if !page.Indexed || page.Algorithm != SimilarityAlgorithm || page.PostID != anchor.ID {
		t.Fatalf("page header=%+v", page)
	}
	byID := make(map[string]int, len(page.Matches))
	for _, match := range page.Matches {
		byID[match.Post.ID] = match.Distance
	}
	nearDistance, ok := byID[near.ID]
	if !ok {
		t.Fatalf("near copy missing from matches: %+v", page.Matches)
	}
	if nearDistance > 8 {
		t.Fatalf("near copy distance=%d want <= 8", nearDistance)
	}
	if _, ok := byID[far.ID]; ok {
		t.Fatalf("unrelated picture matched: %+v", page.Matches)
	}
	if _, ok := byID[anchor.ID]; ok {
		t.Fatal("anchor must be excluded from its own matches")
	}
	previous := -1
	for _, match := range page.Matches {
		if match.Distance < previous {
			t.Fatalf("matches not sorted by distance: %+v", page.Matches)
		}
		previous = match.Distance
	}
}

func TestSimilarityPrivacyAndStaleness(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	anchor := uploadSimilarityFixture(t, backend, ctx, 0, "anchor.jpg")
	hidden := uploadSimilarityFixture(t, backend, ctx, 0, "hidden.jpg")
	trashed := uploadSimilarityFixture(t, backend, ctx, 0, "trashed.jpg")
	if runFingerprintJobs(t, backend) != 3 {
		t.Fatal("expected three fingerprint jobs to run")
	}
	before, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 30, "")
	if err != nil || !before.Indexed {
		t.Fatalf("anchor unindexed: %+v %v", before, err)
	}
	found := false
	for _, match := range before.Matches {
		if match.Post.ID == hidden.ID || match.Post.ID == trashed.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("fixtures should match before hiding/trashing")
	}
	if _, err := backend.ModeratePosts(ctx, "test", ModerationRequest{
		Posts:  []ModerationTarget{{ID: hidden.ID}},
		Action: ModerationActionHide,
	}); err != nil {
		t.Fatalf("hide: %v", err)
	}
	if _, err := backend.Trash(ctx, "test", TrashRequest{PostIDs: []string{trashed.ID}}); err != nil {
		t.Fatalf("trash: %v", err)
	}
	after, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 30, "")
	if err != nil {
		t.Fatalf("ListSimilarPosts: %v", err)
	}
	for _, match := range after.Matches {
		if match.Post.ID == hidden.ID || match.Post.ID == trashed.ID {
			t.Fatalf("hidden/trashed candidate leaked: %+v", after.Matches)
		}
	}
	if _, err := backend.ListSimilarPosts(ctx, hidden.ID, 16, 30, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden anchor err=%v want ErrNotFound", err)
	}
	if _, err := backend.ListSimilarPosts(ctx, trashed.ID, 16, 30, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trashed anchor err=%v want ErrNotFound", err)
	}
	// A stale candidate binding never matches: rewrite the candidate's
	// content hash and confirm it drops out.
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET hash = 'sha256:`+strings.Repeat("e", 64)+`' WHERE id = $1::bigint`, anchor.ID); err != nil {
		t.Fatal(err)
	}
	stale, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 30, "")
	if err != nil {
		t.Fatalf("ListSimilarPosts: %v", err)
	}
	if stale.Indexed || len(stale.Matches) != 0 {
		t.Fatalf("stale anchor must read unindexed and empty: %+v", stale)
	}
}

func TestSimilarityCursorPagingAndBounds(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	anchor := uploadSimilarityFixture(t, backend, ctx, 0, "anchor.jpg")
	for i := 0; i < 3; i++ {
		uploadSimilarityFixture(t, backend, ctx, 0, "copy.jpg")
	}
	if runFingerprintJobs(t, backend) != 4 {
		t.Fatal("expected four fingerprint jobs to run")
	}
	first, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 1, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Matches) != 1 || first.NextCursor == nil {
		t.Fatalf("first page=%+v", first)
	}
	second, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 1, *first.NextCursor)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Matches) == 0 {
		t.Fatalf("second page empty: %+v", second)
	}
	seen := map[string]bool{first.Matches[0].Post.ID: true}
	for _, match := range second.Matches {
		if seen[match.Post.ID] {
			t.Fatalf("page overlap: %+v %+v", first.Matches, second.Matches)
		}
	}
	// Foreign and malformed cursors fail.
	other := uploadSimilarityFixture(t, backend, ctx, 1, "other.jpg")
	if _, err := backend.ListSimilarPosts(ctx, other.ID, 16, 1, *first.NextCursor); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("foreign cursor err=%v", err)
	}
	if _, err := backend.ListSimilarPosts(ctx, anchor.ID, 8, 1, *first.NextCursor); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("radius-changed cursor err=%v", err)
	}
	if _, err := backend.ListSimilarPosts(ctx, anchor.ID, 16, 1, "broken"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor err=%v", err)
	}
}

func TestSimilarityIndexAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	first := uploadSimilarityFixture(t, backend, ctx, 0, "first.jpg")
	second := uploadSimilarityFixture(t, backend, ctx, 0, "second.jpg")
	if _, err := backend.IndexSimilarityPosts(ctx, []string{first.ID, "999999"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("batch with missing id err=%v", err)
	}
	var count int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'fingerprint'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	// Uploads already enqueued one fingerprint job each; the rejected
	// batch must not add more.
	if count != 2 {
		t.Fatalf("rejected batch enqueued jobs: count=%d want 2", count)
	}
	response, err := backend.IndexSimilarityPosts(ctx, []string{first.ID, second.ID})
	if err != nil {
		t.Fatalf("IndexSimilarityPosts: %v", err)
	}
	if len(response.Posts) != 2 {
		t.Fatalf("response=%+v", response)
	}
	retry, err := backend.IndexSimilarityPosts(ctx, []string{first.ID, second.ID})
	if err != nil {
		t.Fatalf("retry IndexSimilarityPosts: %v", err)
	}
	for i := range response.Posts {
		if retry.Posts[i].JobID != response.Posts[i].JobID {
			t.Fatalf("reindex duplicated jobs: %+v vs %+v", response, retry)
		}
	}
}

func TestSimilarityStatusPagesEligibleByID(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	first := uploadSimilarityFixture(t, backend, ctx, 0, "first.jpg")
	second := uploadSimilarityFixture(t, backend, ctx, 1, "second.jpg")
	if runFingerprintJobs(t, backend) != 2 {
		t.Fatal("expected two fingerprint jobs to run")
	}
	full, err := backend.GetSimilarityStatus(ctx, 100, "")
	if err != nil {
		t.Fatalf("GetSimilarityStatus: %v", err)
	}
	if full.EligibleCount != 2 || full.IndexedCount != 2 || full.Algorithm != SimilarityAlgorithm {
		t.Fatalf("status=%+v", full)
	}
	if len(full.Posts) != 2 || full.Posts[0].ID != first.ID || full.Posts[1].ID != second.ID {
		t.Fatalf("status order=%+v", full)
	}
	for _, item := range full.Posts {
		if !item.Indexed {
			t.Fatalf("item unindexed: %+v", item)
		}
	}
	paged, err := backend.GetSimilarityStatus(ctx, 1, "")
	if err != nil {
		t.Fatalf("paged status: %v", err)
	}
	if len(paged.Posts) != 1 || paged.NextCursor == nil {
		t.Fatalf("paged=%+v", paged)
	}
	if paged.EligibleCount != 2 || paged.IndexedCount != 2 {
		t.Fatalf("paged counts drifted: %+v", paged)
	}
	rest, err := backend.GetSimilarityStatus(ctx, 1, *paged.NextCursor)
	if err != nil {
		t.Fatalf("rest status: %v", err)
	}
	if len(rest.Posts) != 1 || rest.Posts[0].ID == paged.Posts[0].ID {
		t.Fatalf("status pages overlap: %+v %+v", paged, rest)
	}
	if _, err := backend.GetSimilarityStatus(ctx, 1, "broken"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor err=%v", err)
	}
	if _, err := backend.GetSimilarityStatus(ctx, 0, ""); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("limit 0 err=%v", err)
	}
}
