package posts

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/health"
)

func TestLibraryHealthInventory(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed library health test")
	}
	ctx := context.Background()
	backend := newUploadTestBackend(t, ctx, url, t.TempDir())
	ready := insertHealthPost(t, ctx, backend, "/media/uploads/ok.png", false)
	missing := insertHealthPost(t, ctx, backend, "/media/uploads/missing.png", false)
	blank := insertHealthPost(t, ctx, backend, "", false)
	deleted := insertHealthPost(t, ctx, backend, "/media/uploads/deleted.png", true)
	if _, err := backend.pool.Exec(ctx, `
INSERT INTO asset_variants (post_id, variant, object_key, url, media_type, width, height, file_size, status)
VALUES ($1::bigint, 'thumb-320', 'derivatives/thumb-320/ok.png', '/media/derivatives/thumb-320/ok.png', 'image/png', 320, 240, 12, 'ready')`, ready); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `
INSERT INTO jobs (kind, idempotency_key, status)
VALUES ('derivative', 'health-failed', 'failed'), ('derivative', 'health-pending', 'pending')`); err != nil {
		t.Fatal(err)
	}

	counts, err := backend.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.MissingThumbnails != 2 || counts.FailedThumbnails != 1 || counts.Backlog != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	originals, err := backend.Originals(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(originals) != 3 {
		t.Fatalf("originals = %d", len(originals))
	}
	var blankKey string
	for _, item := range originals {
		if item.PostID == blank {
			blankKey = item.Key
		}
		if item.PostID == deleted {
			t.Fatal("deleted post was inventoried")
		}
	}
	if blankKey != "" {
		t.Fatal("blank original was treated as a stored object")
	}
	page, err := backend.MissingThumbnails(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.PostIDs) != 2 || page.Truncated || containsID(page.PostIDs, ready) || containsID(page.PostIDs, deleted) || !containsID(page.PostIDs, missing) {
		t.Fatalf("page = %+v", page)
	}
	if _, err := backend.OriginalKey(ctx, blank); !errors.Is(err, health.ErrUnavailable) {
		t.Fatalf("blank key err = %v", err)
	}
	key, err := backend.OriginalKey(ctx, ready)
	if err != nil || key == "" || strings.Contains(key, "\\") {
		t.Fatalf("ready key err = %v", err)
	}
}

func insertHealthPost(t *testing.T, ctx context.Context, backend *PostgresBackend, original string, deleted bool) string {
	t.Helper()
	var id string
	if err := backend.pool.QueryRow(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, deleted_at)
VALUES ('/media/uploads/preview.png', $1, 'image/png', 8, 8, CASE WHEN $2 THEN now() END)
RETURNING id::text`, original, deleted).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
