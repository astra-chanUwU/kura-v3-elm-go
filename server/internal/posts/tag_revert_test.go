package posts

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

// tagRevertTestDatabaseURL returns the disposable database URL for
// postgres-backed tag revert tests, skipping when unset so unit runs stay
// green without a database. Never point it at development data.
func tagRevertTestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed tag revert test")
	}
	return url
}

func newTagRevertTestBackend(t *testing.T, ctx context.Context, url string) *PostgresBackend {
	t.Helper()
	backend, err := NewPostgresBackendWithStore(ctx, url, media.NewLocalStore(t.TempDir()))
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
	if _, err := backend.pool.Exec(ctx, "TRUNCATE posts, jobs, asset_variants CASCADE"); err != nil {
		t.Fatal(err)
	}
	return backend
}

func insertTagTestPost(t *testing.T, ctx context.Context, backend *PostgresBackend, tags []string) string {
	t.Helper()
	var id string
	if err := backend.pool.QueryRow(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, tags)
VALUES ('/media/tag-test.png', '/media/tag-test.png', 'image/png', 10, 10, $1)
RETURNING id::text`, tags).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func editTagsMust(t *testing.T, ctx context.Context, backend *PostgresBackend, id string, version int, add, remove []string) TagEditResult {
	t.Helper()
	response, err := backend.EditTags(ctx, TagEditRequest{
		Posts:  []TagTarget{{ID: id, Version: version}},
		Add:    add,
		Remove: remove,
	})
	if err != nil {
		t.Fatalf("EditTags: %v", err)
	}
	if len(response.Posts) != 1 {
		t.Fatalf("EditTags returned %d results", len(response.Posts))
	}
	return response.Posts[0]
}

func revertTagsMust(t *testing.T, ctx context.Context, backend *PostgresBackend, id string, version, target int) TagEditResult {
	t.Helper()
	response, err := backend.RevertTags(ctx, TagRevertRequest{
		Posts:         []TagTarget{{ID: id, Version: version}},
		TargetVersion: target,
	})
	if err != nil {
		t.Fatalf("RevertTags: %v", err)
	}
	if len(response.Posts) != 1 {
		t.Fatalf("RevertTags returned %d results", len(response.Posts))
	}
	return response.Posts[0]
}

func revisionArraysNonNull(t *testing.T, ctx context.Context, backend *PostgresBackend, id string, version int) {
	t.Helper()
	var addedNull, removedNull, targetNull bool
	if err := backend.pool.QueryRow(ctx, `
SELECT added_tags IS NULL, removed_tags IS NULL, target_tags IS NULL
FROM post_tag_revisions WHERE post_id = $1::bigint AND version = $2`,
		id, version).Scan(&addedNull, &removedNull, &targetNull); err != nil {
		t.Fatalf("read revision %s v%d: %v", id, version, err)
	}
	if addedNull || removedNull {
		t.Fatalf("revision %s v%d stored NULL arrays: addedNull=%v removedNull=%v", id, version, addedNull, removedNull)
	}
}

func equalTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTagDeltaNeverNil pins the NULL-binding contract: every side of the
// delta is a non-nil slice, even when that side is empty, so pgx binds '{}'
// instead of NULL for the NOT NULL revision columns.
func TestTagDeltaNeverNil(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current []string
		target  []string
	}{
		{"unchanged empty", nil, nil},
		{"unchanged", []string{"a"}, []string{"a"}},
		{"add-only", []string{"a"}, []string{"a", "b"}},
		{"remove-only", []string{"a", "b"}, []string{"a"}},
		{"two-sided", []string{"a", "b"}, []string{"b", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := tagDelta(tc.current, tc.target)
			if added == nil || removed == nil {
				t.Fatalf("tagDelta returned nil side: added=%v removed=%v", added, removed)
			}
		})
	}
}

// TestRevertTagsOneSidedAndUnchanged exercises every NULL-prone revert shape
// against real PostgreSQL: add-only, remove-only, two-sided, and
// unchanged-state reverts must all insert immutable revisions with non-NULL
// arrays while preserving optimistic-version bumps.
func TestRevertTagsOneSidedAndUnchanged(t *testing.T) {
	ctx := context.Background()
	backend := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))

	t.Run("add-only", func(t *testing.T) {
		id := insertTagTestPost(t, ctx, backend, []string{"a"})
		editTagsMust(t, ctx, backend, id, 0, []string{"b"}, nil) // v1 [a b]
		editTagsMust(t, ctx, backend, id, 1, nil, []string{"b"}) // v2 [a]
		// Revert to v1 target [a b] from v2 [a]: added=[b], removed=[].
		result := revertTagsMust(t, ctx, backend, id, 2, 1)
		if result.Version != 3 || !equalTags(result.Tags, []string{"a", "b"}) || !result.Changed {
			t.Fatalf("add-only revert wrong: %#v", result)
		}
		revisionArraysNonNull(t, ctx, backend, id, 3)
	})

	t.Run("remove-only", func(t *testing.T) {
		id := insertTagTestPost(t, ctx, backend, []string{"a"})
		editTagsMust(t, ctx, backend, id, 0, []string{"b"}, nil) // v1 [a b]
		editTagsMust(t, ctx, backend, id, 1, []string{"c"}, nil) // v2 [a b c]
		// Revert to v1 target [a b] from v2 [a b c]: removed=[c], added=[].
		result := revertTagsMust(t, ctx, backend, id, 2, 1)
		if result.Version != 3 || !result.Changed {
			t.Fatalf("remove-only revert wrong: %#v", result)
		}
		if !equalTags(result.Tags, []string{"a", "b"}) {
			t.Fatalf("remove-only revert tags=%v want [a b]", result.Tags)
		}
		revisionArraysNonNull(t, ctx, backend, id, 3)
	})

	t.Run("two-sided", func(t *testing.T) {
		id := insertTagTestPost(t, ctx, backend, []string{"a", "b"})
		editTagsMust(t, ctx, backend, id, 0, []string{"c"}, []string{"a"}) // v1 [b c]
		editTagsMust(t, ctx, backend, id, 1, []string{"a"}, []string{"c"}) // v2 [b a]
		result := revertTagsMust(t, ctx, backend, id, 2, 1)                // target [b c]
		if result.Version != 3 || !result.Changed {
			t.Fatalf("two-sided revert wrong: %#v", result)
		}
		if !equalTags(result.Tags, []string{"b", "c"}) {
			t.Fatalf("two-sided revert tags=%v want [b c]", result.Tags)
		}
		revisionArraysNonNull(t, ctx, backend, id, 3)
	})

	t.Run("unchanged", func(t *testing.T) {
		id := insertTagTestPost(t, ctx, backend, []string{"a"})
		editTagsMust(t, ctx, backend, id, 0, []string{"b"}, nil) // v1 [a b]
		result := revertTagsMust(t, ctx, backend, id, 1, 1)      // target is current state
		if result.Version != 2 || result.Changed {
			t.Fatalf("unchanged revert must bump version without Changed: %#v", result)
		}
		if !equalTags(result.Tags, []string{"a", "b"}) {
			t.Fatalf("unchanged revert tags=%v want [a b]", result.Tags)
		}
		revisionArraysNonNull(t, ctx, backend, id, 2)
		// Optimistic version still applies: the same request is now stale.
		if _, err := backend.RevertTags(ctx, TagRevertRequest{
			Posts:         []TagTarget{{ID: id, Version: 1}},
			TargetVersion: 1,
		}); err != ErrConflict {
			t.Fatalf("stale revert: got %v want ErrConflict", err)
		}
	})
}

func TestTagEditsAndRevertsRebuildSearchFromCurrentMetadata(t *testing.T) {
	ctx := context.Background()
	backend := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	var id string
	if err := backend.pool.QueryRow(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, source, artist, tags, search_text)
VALUES ('/media/tag-metadata.png', '/media/tag-metadata.png', 'image/png', 10, 10,
        'cat source', 'cat artist', ARRAY['cat','night'], 'cat night cat source cat artist')
RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	editTagsMust(t, ctx, backend, id, 0, nil, []string{"cat"})
	assertPostSearchText(t, ctx, backend, id, "night cat source cat artist")
	editTagsMust(t, ctx, backend, id, 1, []string{"other"}, nil)
	assertPostSearchText(t, ctx, backend, id, "night other cat source cat artist")
	revertTagsMust(t, ctx, backend, id, 2, 1)
	assertPostSearchText(t, ctx, backend, id, "night cat source cat artist")
}

func assertPostSearchText(t *testing.T, ctx context.Context, backend *PostgresBackend, id, want string) {
	t.Helper()
	var got string
	if err := backend.pool.QueryRow(ctx, `SELECT search_text FROM posts WHERE id=$1::bigint`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("search_text=%q want %q", got, want)
	}
}

func TestTagReactionAndRevertBatchesKeepCallerResultOrder(t *testing.T) {
	ctx := context.Background()
	backend := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	first := insertTagTestPost(t, ctx, backend, []string{"base"})
	second := insertTagTestPost(t, ctx, backend, []string{"base"})
	inputOrder := []string{second, first}
	tagResponse, err := backend.EditTags(ctx, TagEditRequest{
		Posts: []TagTarget{{ID: second, Version: 0}, {ID: first, Version: 0}},
		Add:   []string{"batch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{tagResponse.Posts[0].ID, tagResponse.Posts[1].ID}; !equalTags(got, inputOrder) {
		t.Fatalf("tag result order=%v want caller order %v", got, inputOrder)
	}
	favorite := true
	reactionResponse, err := backend.EditReactions(ctx, ReactionRequest{
		Posts:    []ReactionTarget{{ID: second, Version: 0}, {ID: first, Version: 0}},
		Favorite: &favorite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{reactionResponse.Posts[0].ID, reactionResponse.Posts[1].ID}; !equalTags(got, inputOrder) {
		t.Fatalf("reaction result order=%v want caller order %v", got, inputOrder)
	}
	revertResponse, err := backend.RevertTags(ctx, TagRevertRequest{
		Posts:         []TagTarget{{ID: second, Version: 1}, {ID: first, Version: 1}},
		TargetVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{revertResponse.Posts[0].ID, revertResponse.Posts[1].ID}; !equalTags(got, inputOrder) {
		t.Fatalf("revert result order=%v want caller order %v", got, inputOrder)
	}
}
