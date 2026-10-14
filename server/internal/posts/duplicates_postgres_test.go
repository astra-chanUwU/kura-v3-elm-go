package posts

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

func duplicateTestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed duplicates test")
	}
	if strings.Contains(url, "kura_v3_dev") {
		t.Fatalf("refusing to run duplicates tests against the development library")
	}
	return url
}

func newDuplicateTestBackend(t *testing.T, ctx context.Context, url string) *PostgresBackend {
	t.Helper()
	return newDuplicateTestBackendWithPoolURL(t, ctx, url, url)
}

func newDuplicateTestBackendWithPoolURL(t *testing.T, ctx context.Context, ensureURL, poolURL string) *PostgresBackend {
	t.Helper()
	ensureCollectionTestDatabase(t, ctx, ensureURL)
	backend, err := NewPostgresBackend(ctx, poolURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
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
		if _, err := backend.pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	if _, err := backend.pool.Exec(ctx, "TRUNCATE posts, collections, jobs, asset_variants, tag_operations, tag_redirects CASCADE; UPDATE tag_taxonomy_state SET version=0 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	return backend
}

func duplicateHash(ch byte, n int) string {
	return "sha256:" + strings.Repeat(string([]byte{ch}), 64)
}

func seedDuplicatePost(t *testing.T, ctx context.Context, backend *PostgresBackend, hash, moderation string, deleted bool) string {
	t.Helper()
	deletedExpr := "NULL"
	if deleted {
		deletedExpr = "now()"
	}
	var id string
	err := backend.pool.QueryRow(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, search_text, tags, hash, moderation_state, deleted_at)
VALUES ('/media/dup.png', '/media/dup.png', 'image/png', 10, 10, 'dup', '{}', $1, $2, `+deletedExpr+`)
RETURNING id::text`, hash, moderation).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPostgresDuplicateGroupsKeysetPagesAndPrivacy(t *testing.T) {
	ctx := context.Background()
	backend := newDuplicateTestBackend(t, ctx, duplicateTestDatabaseURL(t))
	hashA := duplicateHash('a', 64)
	hashB := duplicateHash('b', 64)
	// hashA: 4 visible members (samples bounded to 3 oldest).
	var expectedA []string
	for i := 0; i < 4; i++ {
		expectedA = append(expectedA, seedDuplicatePost(t, ctx, backend, hashA, ModerationPublished, false))
	}
	// hashB: 2 visible + 1 hidden + 1 trashed (count stays 2).
	expectedB := []string{
		seedDuplicatePost(t, ctx, backend, hashB, ModerationPublished, false),
		seedDuplicatePost(t, ctx, backend, hashB, ModerationPublished, false),
	}
	seedDuplicatePost(t, ctx, backend, hashB, ModerationHidden, false)
	seedDuplicatePost(t, ctx, backend, hashB, ModerationPublished, true)
	// Singletons, invalid, and blank hashes never form groups.
	seedDuplicatePost(t, ctx, backend, duplicateHash('c', 64), ModerationPublished, false)
	seedDuplicatePost(t, ctx, backend, "bad-hash", ModerationPublished, false)
	seedDuplicatePost(t, ctx, backend, "bad-hash", ModerationPublished, false)
	seedDuplicatePost(t, ctx, backend, "", ModerationPublished, false)
	seedDuplicatePost(t, ctx, backend, "", ModerationPublished, false)

	page, err := backend.ListDuplicateGroups(ctx, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(page.Groups))
	}
	if page.Groups[0].Hash != hashA || page.Groups[1].Hash != hashB {
		t.Fatalf("group order = %q,%q, want hash ASC", page.Groups[0].Hash, page.Groups[1].Hash)
	}
	if page.Groups[0].Count != 4 || page.Groups[1].Count != 2 {
		t.Fatalf("counts = %d,%d, want 4,2", page.Groups[0].Count, page.Groups[1].Count)
	}
	if len(page.Groups[0].Posts) != 3 {
		t.Fatalf("samples = %d, want 3", len(page.Groups[0].Posts))
	}
	for i, post := range page.Groups[0].Posts {
		if post.ID != expectedA[i] {
			t.Fatalf("sample %d = %s, want oldest %s", i, post.ID, expectedA[i])
		}
	}
	if len(page.Groups[1].Posts) != 2 || page.Groups[1].Posts[0].ID != expectedB[0] || page.Groups[1].Posts[1].ID != expectedB[1] {
		t.Fatalf("group B samples = %#v, want %#v", page.Groups[1].Posts, expectedB)
	}
	if page.NextCursor != nil {
		t.Fatalf("unexpected next cursor on full page: %q", *page.NextCursor)
	}

	// Keyset pagination with lookahead: limit 1 yields two pages.
	first, err := backend.ListDuplicateGroups(ctx, 1, nil)
	if err != nil || len(first.Groups) != 1 || first.NextCursor == nil {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	cursor, err := DecodeDuplicateGroupsCursor(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := backend.ListDuplicateGroups(ctx, 1, &cursor)
	if err != nil || len(second.Groups) != 1 || second.Groups[0].Hash != hashB || second.NextCursor != nil {
		t.Fatalf("second page = %#v, err=%v", second, err)
	}
}

func TestPostgresDuplicateMembersKeysetPagesAndShrinking(t *testing.T) {
	ctx := context.Background()
	backend := newDuplicateTestBackend(t, ctx, duplicateTestDatabaseURL(t))
	hash := duplicateHash('d', 64)
	var ids []string
	for i := 0; i < 5; i++ {
		ids = append(ids, seedDuplicatePost(t, ctx, backend, hash, ModerationPublished, false))
	}
	seedDuplicatePost(t, ctx, backend, hash, ModerationHidden, false)
	seedDuplicatePost(t, ctx, backend, hash, ModerationPublished, true)

	first, err := backend.ListDuplicateMembers(ctx, hash, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != hash || first.Count != 5 || len(first.Posts) != 2 || first.NextCursor == nil {
		t.Fatalf("first members page = %#v", first)
	}
	if first.Posts[0].ID != ids[0] || first.Posts[1].ID != ids[1] {
		t.Fatalf("member order = %q,%q, want id ASC", first.Posts[0].ID, first.Posts[1].ID)
	}
	cursor, err := DecodeDuplicateMembersCursor(*first.NextCursor, hash)
	if err != nil {
		t.Fatal(err)
	}
	second, err := backend.ListDuplicateMembers(ctx, hash, 2, &cursor)
	if err != nil || len(second.Posts) != 2 || second.Count != 5 || second.NextCursor == nil {
		t.Fatalf("second members page = %#v, err=%v", second, err)
	}
	cursor2, err := DecodeDuplicateMembersCursor(*second.NextCursor, hash)
	if err != nil {
		t.Fatal(err)
	}
	third, err := backend.ListDuplicateMembers(ctx, hash, 2, &cursor2)
	if err != nil || len(third.Posts) != 1 || third.Posts[0].ID != ids[4] || third.NextCursor != nil {
		t.Fatalf("third members page = %#v, err=%v", third, err)
	}
	// Foreign-hash cursor is rejected without touching the database snapshot.
	otherCursor, err := EncodeDuplicateMembersCursor(duplicateHash('e', 64), 1)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := DecodeDuplicateMembersCursor(otherCursor, duplicateHash('e', 64))
	if _, err := backend.ListDuplicateMembers(ctx, hash, 2, &foreign); err != ErrInvalidCursor {
		t.Fatalf("foreign cursor: got %v, want ErrInvalidCursor", err)
	}

	// Shrinking the visible group below two reads as missing.
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE hash = $1 AND deleted_at IS NULL AND moderation_state = 'published' AND id::text <> $2`, hash, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET moderation_state = 'hidden' WHERE id = $1::bigint`, ids[1]); err != nil {
		t.Fatal(err)
	}
	// Four hidden/trashed leaves one visible member.
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE hash = $1 AND deleted_at IS NULL AND id::text <> $2`, hash, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ListDuplicateMembers(ctx, hash, 60, nil); err != ErrNotFound {
		t.Fatalf("shrunk group: got %v, want ErrNotFound", err)
	}
	groups, err := backend.ListDuplicateGroups(ctx, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups.Groups {
		if group.Hash == hash {
			t.Fatalf("shrunk hash still listed: %#v", group)
		}
	}
}

func TestPostgresDuplicateThumbnailOverlay(t *testing.T) {
	ctx := context.Background()
	backend := newDuplicateTestBackend(t, ctx, duplicateTestDatabaseURL(t))
	hash := duplicateHash('f', 64)
	idA := seedDuplicatePost(t, ctx, backend, hash, ModerationPublished, false)
	seedDuplicatePost(t, ctx, backend, hash, ModerationPublished, false)
	readyURL := "/media/derivatives/thumb-320/dup-overlay.jpg"
	if _, err := backend.pool.Exec(ctx, `INSERT INTO asset_variants (post_id, variant, object_key, url, media_type, width, height, file_size, status) VALUES ($1::bigint, 'thumb-320', 'derivatives/thumb-320/dup-overlay.jpg', $2, 'image/jpeg', 320, 240, 100, 'ready')`, idA, readyURL); err != nil {
		t.Fatal(err)
	}
	groups, err := backend.ListDuplicateGroups(ctx, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups.Groups) != 1 || len(groups.Groups[0].Posts) != 2 {
		t.Fatalf("overlay groups = %#v", groups)
	}
	if groups.Groups[0].Posts[0].PreviewURL != readyURL {
		t.Fatalf("group sample overlay = %q, want %q", groups.Groups[0].Posts[0].PreviewURL, readyURL)
	}
	if groups.Groups[0].Posts[0].OriginalURL == readyURL {
		t.Fatal("overlay rewrote the original URL")
	}
	members, err := backend.ListDuplicateMembers(ctx, hash, 60, nil)
	if err != nil {
		t.Fatal(err)
	}
	if members.Posts[0].PreviewURL != readyURL || members.Posts[0].OriginalURL == readyURL {
		t.Fatalf("member overlay = %#v", members.Posts[0])
	}
	if !strings.HasPrefix(members.Posts[1].PreviewURL, "/media/dup") {
		t.Fatalf("processing member lost original preview: %q", members.Posts[1].PreviewURL)
	}
}

func TestPostgresDuplicatesSucceedWithSingleConnection(t *testing.T) {
	url := duplicateTestDatabaseURL(t)
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backend := newDuplicateTestBackendWithPoolURL(t, ctx, url, config.ConnString())
	hash := duplicateHash('a', 64)
	idA := seedDuplicatePost(t, ctx, backend, hash, ModerationPublished, false)
	seedDuplicatePost(t, ctx, backend, hash, ModerationPublished, false)
	readyURL := "/media/derivatives/thumb-320/dup-single.jpg"
	if _, err := backend.pool.Exec(ctx, `INSERT INTO asset_variants (post_id, variant, object_key, url, media_type, width, height, file_size, status) VALUES ($1::bigint, 'thumb-320', 'derivatives/thumb-320/dup-single.jpg', $2, 'image/jpeg', 320, 240, 100, 'ready')`, idA, readyURL); err != nil {
		t.Fatal(err)
	}
	groups, err := backend.ListDuplicateGroups(ctx, 20, nil)
	if err != nil {
		t.Fatalf("single-connection groups read failed: %v", err)
	}
	if len(groups.Groups) != 1 || groups.Groups[0].Posts[0].PreviewURL != readyURL {
		t.Fatalf("single-connection groups = %#v", groups)
	}
	members, err := backend.ListDuplicateMembers(ctx, hash, 60, nil)
	if err != nil {
		t.Fatalf("single-connection members read failed: %v", err)
	}
	if len(members.Posts) != 2 || members.Posts[0].PreviewURL != readyURL {
		t.Fatalf("single-connection members = %#v", members)
	}
}
