package posts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// collectionPaginationTestURL returns the disposable database URL for
// versioned-collection pagination tests, skipping when unset so unit runs
// stay green without a database. It refuses the development library name
// so a misconfigured environment cannot truncate or reseed real data.
func collectionPaginationTestURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed collection pagination test")
	}
	if strings.Contains(url, "kura_v3_dev") {
		t.Fatalf("refusing to run collection pagination tests against the development library")
	}
	return url
}

func ensureCollectionTestDatabase(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	name := config.ConnConfig.Database
	if name == "" {
		t.Fatal("test database URL has no database name")
	}
	config.ConnConfig.Database = "postgres"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE DATABASE `+pgxIdentifier(name)); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42P04" {
			t.Fatalf("create test database: %v", err)
		}
	}
}

func pgxIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func newCollectionPaginationSearcher(t *testing.T, ctx context.Context, url string) *PostgresSearcher {
	t.Helper()
	return newCollectionPaginationSearcherWithPoolURL(t, ctx, url, url)
}

func newCollectionPaginationSearcherWithPoolURL(t *testing.T, ctx context.Context, ensureURL string, poolURL string) *PostgresSearcher {
	t.Helper()
	ensureCollectionTestDatabase(t, ctx, ensureURL)
	searcher, err := NewPostgresSearcher(ctx, poolURL)
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
	if _, err := searcher.pool.Exec(ctx, "TRUNCATE posts, collections CASCADE"); err != nil {
		t.Fatal(err)
	}
	return searcher
}

// seedPublishedPosts inserts n visible posts and returns their IDs in
// insertion order. Defaults keep every row published and non-deleted.
func seedPublishedPosts(t *testing.T, ctx context.Context, searcher *PostgresSearcher, n int) []string {
	t.Helper()
	rows, err := searcher.pool.Query(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, search_text, tags)
SELECT '/media/seed/' || g || '.jpg', '/media/seed/' || g || '-orig.jpg', 'image/jpeg', 640, 480, 'seed', '{}'
FROM generate_series(1, $1) g
RETURNING id::text`, n)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := make([]string, 0, n)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != n {
		t.Fatalf("seeded %d posts, want %d", len(ids), n)
	}
	return ids
}

func addCollectionPostsInChunks(t *testing.T, ctx context.Context, searcher *PostgresSearcher, collectionID string, ids []string, chunk int) Collection {
	t.Helper()
	var collection Collection
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		var err error
		collection, err = searcher.AddCollectionPosts(ctx, collectionID, AddCollectionPostsRequest{PostIDs: ids[start:end]})
		if err != nil {
			t.Fatal(err)
		}
	}
	return collection
}

func collectionVersion(t *testing.T, ctx context.Context, searcher *PostgresSearcher, collectionID string) (Collection, int64) {
	t.Helper()
	collection, err := searcher.collectionByID(ctx, collectionID)
	if err != nil {
		t.Fatal(err)
	}
	return collection, collection.Version
}

// traverseCollection walks every page at the given limit, decoding the
// opaque cursor between pages exactly like an HTTP client would. It
// returns the visited IDs in order and every page's observed version.
func traverseCollection(t *testing.T, ctx context.Context, searcher *PostgresSearcher, collectionID string, limit int) ([]string, []int64) {
	t.Helper()
	parsed, err := strconv.ParseInt(collectionID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var versions []int64
	var cursor *CollectionCursor
	for {
		page, err := searcher.SearchCollectionPosts(ctx, collectionID, cursor, limit)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, page.CollectionVersion)
		for _, post := range page.Posts {
			ids = append(ids, post.ID)
		}
		if page.NextCursor == nil {
			return ids, versions
		}
		decoded, err := DecodeCollectionCursor(*page.NextCursor, parsed)
		if err != nil {
			t.Fatalf("server-issued cursor rejected: %v", err)
		}
		next := decoded
		cursor = &next
	}
}

func assertTraversal(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("traversed %d members, want %d", len(got), len(want))
	}
	seen := make(map[string]int, len(got))
	for _, id := range got {
		seen[id]++
		if seen[id] > 1 {
			t.Fatalf("duplicate member %q in traversal", id)
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func assertStableVersion(t *testing.T, versions []int64) int64 {
	t.Helper()
	if len(versions) == 0 {
		t.Fatal("no pages observed")
	}
	for _, version := range versions[1:] {
		if version != versions[0] {
			t.Fatalf("version moved mid-traversal: %v", versions)
		}
	}
	return versions[0]
}

func TestCollectionPaginationTraverses150Members(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 150)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Large"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 0 {
		t.Fatalf("new collection version=%d want 0", created.Version)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids, 200)

	visited, versions := traverseCollection(t, ctx, searcher, created.ID, 60)
	assertTraversal(t, visited, ids)
	version := assertStableVersion(t, versions)
	if len(versions) != 3 {
		t.Fatalf("150 members at limit 60 took %d pages, want 3", len(versions))
	}
	if _, current := collectionVersion(t, ctx, searcher, created.ID); current != version {
		t.Fatalf("traversal version=%d differs from current=%d", version, current)
	}

	// A small page size walks the same total order with more round trips.
	visited, versions = traverseCollection(t, ctx, searcher, created.ID, 7)
	assertTraversal(t, visited, ids)
	assertStableVersion(t, versions)
	if len(versions) != 22 {
		t.Fatalf("150 members at limit 7 took %d pages, want 22", len(versions))
	}
}

func TestCollectionPaginationTraverses1000Members(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 1000)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Huge"})
	if err != nil {
		t.Fatal(err)
	}
	// The 200-ID bound is per request: five requests grow one collection
	// far past any single-request size.
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids, 200)

	visited, versions := traverseCollection(t, ctx, searcher, created.ID, 60)
	assertTraversal(t, visited, ids)
	assertStableVersion(t, versions)
	if len(versions) != 17 {
		t.Fatalf("1000 members at limit 60 took %d pages, want 17", len(versions))
	}

	// Full-membership reorder carries 1,000 IDs in one request: there is no
	// total-size cap, and the reversed order traverses exactly.
	reversed := append([]string(nil), ids...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if _, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: reversed}); err != nil {
		t.Fatalf("reorder 1000: %v", err)
	}
	visited, _ = traverseCollection(t, ctx, searcher, created.ID, 60)
	assertTraversal(t, visited, reversed)
}

func TestCollectionPaginationPositionTies(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 10)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Ties"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids, 200)
	// Tie a middle block so the expected order differs from insertion.
	tied := append([]string(nil), ids[2:8]...)
	if _, err := searcher.pool.Exec(ctx, `UPDATE collection_posts SET position = 0 WHERE collection_id = $1::bigint AND post_id = ANY($2::bigint[])`, created.ID, postIDInts(t, tied)); err != nil {
		t.Fatal(err)
	}

	// Ties share one total order: (position, post_id) keeps the tied block
	// contiguous and sorted by numeric post id, ahead of later positions.
	inTied := make(map[string]bool, len(tied))
	for _, id := range tied {
		inTied[id] = true
	}
	numeric := append([]string(nil), tied...)
	sort.Slice(numeric, func(i, j int) bool {
		a, _ := strconv.ParseInt(numeric[i], 10, 64)
		b, _ := strconv.ParseInt(numeric[j], 10, 64)
		return a < b
	})
	want := append([]string(nil), numeric...)
	for _, id := range ids {
		if !inTied[id] {
			want = append(want, id)
		}
	}
	visited, versions := traverseCollection(t, ctx, searcher, created.ID, 3)
	assertTraversal(t, visited, want)
	assertStableVersion(t, versions)

	// The tie order is deterministic across traversals.
	again, _ := traverseCollection(t, ctx, searcher, created.ID, 4)
	assertTraversal(t, again, want)
}

func postIDInts(t *testing.T, ids []string) []int64 {
	t.Helper()
	ints := make([]int64, len(ids))
	for i, id := range ids {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		ints[i] = parsed
	}
	return ints
}

func TestCollectionPaginationRejectsStaleVersion(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 5)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Stale"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids, 200)
	first, err := searcher.SearchCollectionPosts(ctx, created.ID, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == nil {
		t.Fatal("expected a next cursor on a partial page")
	}
	parsed, _ := strconv.ParseInt(created.ID, 10, 64)
	stale, err := DecodeCollectionCursor(*first.NextCursor, parsed)
	if err != nil {
		t.Fatal(err)
	}

	extra := seedPublishedPosts(t, ctx, searcher, 1)
	if _, err := searcher.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: extra}); err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.SearchCollectionPosts(ctx, created.ID, &stale, 2); err != ErrCollectionChanged {
		t.Fatalf("stale cursor: expected ErrCollectionChanged, got %v", err)
	}

	// A fresh first page at the new version resumes traversal.
	fresh, err := searcher.SearchCollectionPosts(ctx, created.ID, nil, 60)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.CollectionVersion != first.CollectionVersion+1 {
		t.Fatalf("version=%d want %d", fresh.CollectionVersion, first.CollectionVersion+1)
	}
	if len(fresh.Posts) != 6 || fresh.NextCursor != nil {
		t.Fatalf("unexpected resumed page: %d posts cursor=%v", len(fresh.Posts), fresh.NextCursor)
	}
}

func TestCollectionSearcherRejectsCrossCollectionCursor(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	seedPublishedPosts(t, ctx, searcher, 2)
	first, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := strconv.ParseInt(second.ID, 10, 64)
	token, err := EncodeCollectionCursor(otherID, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := strconv.ParseInt(first.ID, 10, 64)
	if _, err := DecodeCollectionCursor(token, firstID); err != ErrInvalidCursor {
		t.Fatalf("cross-collection token: expected ErrInvalidCursor, got %v", err)
	}
	foreign := CollectionCursor{CollectionID: otherID, Version: 0, Position: 0, PostID: 1}
	if _, err := searcher.SearchCollectionPosts(ctx, first.ID, &foreign, 60); err != ErrInvalidCursor {
		t.Fatalf("foreign cursor struct: expected ErrInvalidCursor, got %v", err)
	}
}

func TestCollectionVersionOnlyEffectiveChanges(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 6)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Versions"})
	if err != nil {
		t.Fatal(err)
	}
	version := created.Version

	bump := func(name string, want int64, action func() error) {
		t.Helper()
		if err := action(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, current := collectionVersion(t, ctx, searcher, created.ID); current != want {
			t.Fatalf("%s: version=%d want %d", name, current, want)
		}
		version = want
	}

	// Effective add bumps once per request, not per row.
	bump("add two", version+1, func() error {
		_, err := searcher.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: ids[:2]})
		return err
	})
	// Re-adding current members is a no-op: the version stays stable.
	bump("add duplicates", version, func() error {
		_, err := searcher.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: ids[:2]})
		return err
	})
	// A partially new request still bumps exactly once.
	bump("add mixed", version+1, func() error {
		_, err := searcher.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: []string{ids[1], ids[2]}})
		return err
	})
	// Removing a non-member succeeds without touching the version.
	bump("remove absent", version, func() error {
		return searcher.RemoveCollectionPost(ctx, created.ID, ids[5])
	})
	bump("remove member", version+1, func() error {
		return searcher.RemoveCollectionPost(ctx, created.ID, ids[0])
	})
	// Reordering into the current order is a no-op.
	bump("reorder same", version, func() error {
		_, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: []string{ids[1], ids[2]}})
		return err
	})
	bump("reorder changed", version+1, func() error {
		_, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: []string{ids[2], ids[1]}})
		return err
	})
	// Hiding a member is a moderation change, not a collection change: the
	// membership version stays stable while the page hides the row.
	page, err := searcher.SearchCollectionPosts(ctx, created.ID, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.ModeratePosts(ctx, "test", ModerationRequest{Posts: []ModerationTarget{{ID: ids[1]}}, Action: ModerationActionHide}); err != nil {
		t.Fatal(err)
	}
	bump("hide member", version, func() error { return nil })
	parsed, _ := strconv.ParseInt(created.ID, 10, 64)
	cursor, err := DecodeCollectionCursor(*page.NextCursor, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.SearchCollectionPosts(ctx, created.ID, &cursor, 1); err != nil {
		t.Fatalf("pre-hide cursor after hide: %v", err)
	}
}

func TestCollectionConcurrentMutations(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 20)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Racy"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids[:8], 200)
	_, base := collectionVersion(t, ctx, searcher, created.ID)

	// Twelve concurrent single-row adds serialize on the collection row:
	// every insert lands and the version counts every effective call.
	var group sync.WaitGroup
	failures := make(chan error, 12)
	for _, id := range ids[8:] {
		group.Add(1)
		go func(postID string) {
			defer group.Done()
			_, err := searcher.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: []string{postID}})
			if err != nil {
				failures <- err
			}
		}(id)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatalf("concurrent add: %v", err)
	}
	if _, current := collectionVersion(t, ctx, searcher, created.ID); current != base+12 {
		t.Fatalf("version=%d want %d after 12 concurrent adds", current, base+12)
	}
	visited, _ := traverseCollection(t, ctx, searcher, created.ID, 60)
	if len(visited) != 20 {
		t.Fatalf("traversed %d members, want 20 after concurrent adds", len(visited))
	}

	// Two concurrent full reorders both commit with no lost update: each
	// request differs from the current order, so both bump the version and
	// the final order is exactly one of the two requests.
	rotated := append(append([]string(nil), visited[1:]...), visited[0])
	backward := append([]string(nil), visited...)
	for i, j := 0, len(backward)-1; i < j; i, j = i+1, j-1 {
		backward[i], backward[j] = backward[j], backward[i]
	}
	forward := rotated
	_, before := collectionVersion(t, ctx, searcher, created.ID)
	group.Add(2)
	reorderErrs := make(chan error, 2)
	go func() {
		defer group.Done()
		_, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: forward})
		if err != nil {
			reorderErrs <- err
		}
	}()
	go func() {
		defer group.Done()
		_, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: backward})
		if err != nil {
			reorderErrs <- err
		}
	}()
	group.Wait()
	close(reorderErrs)
	for err := range reorderErrs {
		t.Fatalf("concurrent reorder: %v", err)
	}
	final, after := collectionVersion(t, ctx, searcher, created.ID)
	if after != before+2 {
		t.Fatalf("version=%d want %d after two concurrent reorders", after, before+2)
	}
	joined := strings.Join(final.PostIDs, ",")
	if joined != strings.Join(forward, ",") && joined != strings.Join(backward, ",") {
		t.Fatalf("final order is neither reorder request: %v", final.PostIDs)
	}
}

func TestCollectionPaginationVisibilityAndThumbnails(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 5)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Visible"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids, 200)

	readyURL := "/media/derivatives/thumb-320/ready.jpg"
	if _, err := searcher.pool.Exec(ctx, `INSERT INTO asset_variants (post_id, variant, object_key, url, media_type, width, height, file_size, status) VALUES ($1::bigint, 'thumb-320', 'derivatives/thumb-320/ready.jpg', $2, 'image/jpeg', 320, 240, 100, 'ready')`, ids[0], readyURL); err != nil {
		t.Fatal(err)
	}
	page, err := searcher.SearchCollectionPosts(ctx, created.ID, nil, 60)
	if err != nil {
		t.Fatal(err)
	}
	if page.Posts[0].PreviewURL != readyURL {
		t.Fatalf("ready thumbnail not overlaid: %q", page.Posts[0].PreviewURL)
	}
	if page.Posts[0].OriginalURL == readyURL {
		t.Fatal("thumbnail overlay rewrote the original URL")
	}
	for _, post := range page.Posts[1:] {
		if !strings.HasPrefix(post.PreviewURL, "/media/seed/") {
			t.Fatalf("processing post lost its original preview: %q", post.PreviewURL)
		}
	}

	// Hidden and soft-deleted members read as missing while the version and
	// the remaining traversal stay complete.
	if _, err := searcher.ModeratePosts(ctx, "test", ModerationRequest{Posts: []ModerationTarget{{ID: ids[1]}}, Action: ModerationActionHide}); err != nil {
		t.Fatal(err)
	}
	if _, err := searcher.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE id = $1::bigint`, ids[2]); err != nil {
		t.Fatal(err)
	}
	_, version := collectionVersion(t, ctx, searcher, created.ID)
	visited, versions := traverseCollection(t, ctx, searcher, created.ID, 2)
	assertStableVersion(t, versions)
	if versions[0] != version {
		t.Fatalf("moderation moved collection version to %d", versions[0])
	}
	assertTraversal(t, visited, []string{ids[0], ids[3], ids[4]})
	if page.NextCursor != nil {
		t.Fatal("expected null cursor on a complete small collection")
	}
}

func TestCollectionMutationFailuresLeaveNoPartialState(t *testing.T) {
	ctx := context.Background()
	searcher := newCollectionPaginationSearcher(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, searcher, 4)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "Atomic"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids[:3], 200)
	before, version := collectionVersion(t, ctx, searcher, created.ID)

	// A reorder missing one member aborts before writing any position.
	if _, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: ids[:2]}); err != ErrInvalidCollections {
		t.Fatalf("short reorder: expected ErrInvalidCollections, got %v", err)
	}
	// A reorder naming an unknown member aborts the same way.
	if _, err := searcher.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: []string{ids[0], ids[1], ids[3]}}); err != ErrInvalidCollections {
		t.Fatalf("foreign reorder: expected ErrInvalidCollections, got %v", err)
	}
	// Malformed input never reaches the database.
	if _, err := searcher.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: []string{"0"}}); err == nil {
		t.Fatal("invalid add accepted")
	}
	if err := searcher.RemoveCollectionPost(ctx, "999999999", ids[0]); err != ErrNotFound {
		t.Fatalf("remove from missing collection: expected ErrNotFound, got %v", err)
	}
	after, current := collectionVersion(t, ctx, searcher, created.ID)
	if current != version || strings.Join(after.PostIDs, ",") != strings.Join(before.PostIDs, ",") {
		t.Fatalf("failed mutations changed the collection: %#v", after)
	}
	visited, _ := traverseCollection(t, ctx, searcher, created.ID, 60)
	assertTraversal(t, visited, ids[:3])
}

// TestCollectionPageSucceedsWithSingleConnection proves a non-empty
// collection page, including the ready-thumbnail overlay, needs only one
// pooled connection. The page read holds its snapshot transaction while
// overlaying, so borrowing a second connection used to deadlock a pool
// of one. The deadline turns that stall into a failure instead of a hang.
func TestCollectionPageSucceedsWithSingleConnection(t *testing.T) {
	url := collectionPaginationTestURL(t)
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	searcher := newCollectionPaginationSearcherWithPoolURL(t, ctx, url, config.ConnString())
	ids := seedPublishedPosts(t, ctx, searcher, 3)
	created, err := searcher.CreateCollection(ctx, CreateCollectionRequest{Name: "SingleConn"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, searcher, created.ID, ids, 200)

	readyURL := "/media/derivatives/thumb-320/single-conn.jpg"
	if _, err := searcher.pool.Exec(ctx, `INSERT INTO asset_variants (post_id, variant, object_key, url, media_type, width, height, file_size, status) VALUES ($1::bigint, 'thumb-320', 'derivatives/thumb-320/single-conn.jpg', $2, 'image/jpeg', 320, 240, 100, 'ready')`, ids[0], readyURL); err != nil {
		t.Fatal(err)
	}
	page, err := searcher.SearchCollectionPosts(ctx, created.ID, nil, 60)
	if err != nil {
		t.Fatalf("single-connection page read failed: %v", err)
	}
	if len(page.Posts) != 3 {
		t.Fatalf("page holds %d posts, want 3", len(page.Posts))
	}
	if page.Posts[0].PreviewURL != readyURL {
		t.Fatalf("ready thumbnail not overlaid: %q", page.Posts[0].PreviewURL)
	}
	if page.Posts[0].OriginalURL == readyURL {
		t.Fatal("thumbnail overlay rewrote the original URL")
	}
	for _, post := range page.Posts[1:] {
		if !strings.HasPrefix(post.PreviewURL, "/media/seed/") {
			t.Fatalf("processing post lost its original preview: %q", post.PreviewURL)
		}
	}
}
