package posts

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

func TestValidateMetadataRequest(t *testing.T) {
	empty := ""
	artist := "  ねこ  "
	got, err := ValidateMetadataRequest(MetadataRequest{
		Posts:  []MetadataTarget{{ID: "2004", Version: 0}},
		Source: &empty,
		Artist: &artist,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source == nil || *got.Source != "" || got.Artist == nil || *got.Artist != "ねこ" {
		t.Fatalf("unexpected normalized metadata: %#v", got)
	}

	tooLong := strings.Repeat("猫", 513)
	for _, request := range []MetadataRequest{
		{Posts: []MetadataTarget{{ID: "2004", Version: 0}}},
		{Posts: []MetadataTarget{{ID: "02004", Version: 0}}, Source: &empty},
		{Posts: []MetadataTarget{{ID: "2004", Version: -1}}, Source: &empty},
		{Posts: []MetadataTarget{{ID: "2004", Version: 0}, {ID: "2004", Version: 0}}, Source: &empty},
		{Posts: []MetadataTarget{{ID: "2004", Version: 0}}, Artist: &tooLong},
	} {
		if _, err := ValidateMetadataRequest(request); !errors.Is(err, ErrInvalidMetadata) {
			t.Errorf("request %#v: got %v, want ErrInvalidMetadata", request, err)
		}
	}
}

func TestMetadataTargetRequiresExplicitVersion(t *testing.T) {
	for _, body := range []string{`{"id":"2004"}`, `{"id":"2004","version":null}`, `{"id":"2004","version":-1}`, `{"id":"2004","version":0.5}`, `{"id":"2004","version":0,"extra":true}`} {
		var target MetadataTarget
		if err := json.Unmarshal([]byte(body), &target); !errors.Is(err, ErrInvalidMetadata) {
			t.Errorf("%s: got %v, want ErrInvalidMetadata", body, err)
		}
	}
	var zero MetadataTarget
	if err := json.Unmarshal([]byte(`{"id":"2004","version":0}`), &zero); err != nil || zero.Version != 0 {
		t.Fatalf("version zero should be valid: target=%#v err=%v", zero, err)
	}
}

func TestCanonicalSearchText(t *testing.T) {
	if got, want := canonicalSearchText([]string{"night", "cute cat"}, "source", "artist"), "night cute cat source artist"; got != want {
		t.Fatalf("canonicalSearchText=%q want %q", got, want)
	}
	if got, want := canonicalSearchText([]string{"night"}, "", ""), "night"; got != want {
		t.Fatalf("empty metadata left search terms behind: %q", got)
	}
}

func metadataTestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("KURA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set; skipping postgres-backed metadata test")
	}
	return url
}

func newMetadataTestBackend(t *testing.T, ctx context.Context, url string) *PostgresBackend {
	t.Helper()
	backend, err := NewPostgresBackendWithStore(ctx, url, media.NewLocalStore(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
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

func TestPostgresMetadataEditAtomicHistoryAndSearchText(t *testing.T) {
	ctx := context.Background()
	backend := newMetadataTestBackend(t, ctx, metadataTestDatabaseURL(t))
	ids := make([]string, 2)
	for index := range ids {
		if err := backend.pool.QueryRow(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, source, artist, tags, search_text)
VALUES ('/media/meta.png', '/media/meta.png', 'image/png', 10, 10, 'old source', 'old artist', ARRAY['night','cute cat'], 'night cute cat old source old artist')
RETURNING id::text`).Scan(&ids[index]); err != nil {
			t.Fatal(err)
		}
	}

	request := MetadataRequest{Posts: []MetadataTarget{{ID: ids[0], Version: 0}, {ID: ids[1], Version: 1}}, Source: strPtr("new source"), Artist: strPtr("new artist")}
	if _, err := backend.UpdateMetadata(ctx, request); !errors.Is(err, ErrMetadataConflict) {
		t.Fatalf("stale batch err=%v want ErrMetadataConflict", err)
	}
	for _, id := range ids {
		var source, artist string
		var version int
		if err := backend.pool.QueryRow(ctx, `SELECT source, artist, metadata_version FROM posts WHERE id=$1::bigint`, id).Scan(&source, &artist, &version); err != nil {
			t.Fatal(err)
		}
		if source != "old source" || artist != "old artist" || version != 0 {
			t.Fatalf("stale batch partially changed post %s: %q %q v%d", id, source, artist, version)
		}
	}

	request.Posts[1].Version = 0
	result, err := backend.UpdateMetadata(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Posts) != 2 || !result.Posts[0].Changed || !result.Posts[1].Changed || result.Posts[0].Version != 1 {
		t.Fatalf("unexpected update result: %#v", result)
	}
	var searchText string
	if err := backend.pool.QueryRow(ctx, `SELECT search_text FROM posts WHERE id=$1::bigint`, ids[0]).Scan(&searchText); err != nil {
		t.Fatal(err)
	}
	if searchText != "night cute cat new source new artist" {
		t.Fatalf("search_text=%q", searchText)
	}

	// A stale request that would otherwise be a value-level no-op must still
	// conflict because the optimistic version check precedes no-op detection.
	staleNoop := MetadataRequest{
		Posts:  []MetadataTarget{{ID: ids[0], Version: 0}},
		Source: strPtr("new source"),
		Artist: strPtr("new artist"),
	}
	if _, err := backend.UpdateMetadata(ctx, staleNoop); !errors.Is(err, ErrMetadataConflict) {
		t.Fatalf("stale no-op err=%v want ErrMetadataConflict", err)
	}

	// Repeating the exact same edit at the current version is a no-op.
	currentNoop := MetadataRequest{
		Posts:  []MetadataTarget{{ID: ids[0], Version: 1}},
		Source: strPtr("new source"),
		Artist: strPtr("new artist"),
	}
	result, err = backend.UpdateMetadata(ctx, currentNoop)
	if err != nil {
		t.Fatal(err)
	}
	if result.Posts[0].Changed || result.Posts[0].Version != 1 {
		t.Fatalf("same-value edit should be a no-op: %#v", result.Posts[0])
	}
	var auditCount int
	if err := backend.pool.QueryRow(ctx, `SELECT COUNT(*) FROM post_metadata_revisions WHERE post_id=$1::bigint`, ids[0]).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit rows=%d want 1", auditCount)
	}

	detail, err := backend.GetPostDetail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if detail.MetadataVersion != 1 || len(detail.MetadataHistory) != 1 || detail.MetadataHistory[0].PreviousSource != "old source" || detail.MetadataHistory[0].Source != "new source" {
		t.Fatalf("detail metadata history mismatch: version=%d history=%#v", detail.MetadataVersion, detail.MetadataHistory)
	}
	if !reflect.DeepEqual(detail.Tags, []string{"night", "cute cat"}) {
		t.Fatalf("metadata edit changed tags: %#v", detail.Tags)
	}

	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET moderation_state='hidden' WHERE id=$1::bigint`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.UpdateMetadata(ctx, MetadataRequest{Posts: []MetadataTarget{{ID: ids[0], Version: 1}}, Source: strPtr("hidden edit")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden post err=%v want ErrNotFound", err)
	}
}

func TestPostgresConcurrentMetadataTagAndReactionBatchesLockInOneOrder(t *testing.T) {
	backend := newMetadataTestBackend(t, context.Background(), metadataTestDatabaseURL(t))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	ids := make([]string, 2)
	for index := range ids {
		if err := backend.pool.QueryRow(ctx, `
INSERT INTO posts (preview_url, original_url, media_type, width, height, source, artist, tags, search_text)
VALUES ('/media/concurrent-meta.png', '/media/concurrent-meta.png', 'image/png', 10, 10,
        'old source', 'old artist', ARRAY['base'], 'base old source old artist')
RETURNING id::text`).Scan(&ids[index]); err != nil {
			t.Fatal(err)
		}
	}
	if ids[0] >= ids[1] {
		t.Fatalf("test posts are not in numeric order: %v", ids)
	}

	start := make(chan struct{})
	type mutationResult struct {
		name string
		err  error
	}
	results := make(chan mutationResult, 3)
	go func() {
		<-start
		_, err := backend.UpdateMetadata(ctx, MetadataRequest{
			Posts:  []MetadataTarget{{ID: ids[1], Version: 0}, {ID: ids[0], Version: 0}},
			Source: strPtr("current source"),
			Artist: strPtr("current artist"),
		})
		results <- mutationResult{name: "metadata", err: err}
	}()
	go func() {
		<-start
		_, err := backend.EditTags(ctx, TagEditRequest{
			Posts: []TagTarget{{ID: ids[0], Version: 0}, {ID: ids[1], Version: 0}},
			Add:   []string{"current tag"},
		})
		results <- mutationResult{name: "tags", err: err}
	}()
	go func() {
		<-start
		favorite := true
		_, err := backend.EditReactions(ctx, ReactionRequest{
			Posts:    []ReactionTarget{{ID: ids[0], Version: 0}, {ID: ids[1], Version: 0}},
			Favorite: &favorite,
		})
		results <- mutationResult{name: "reactions", err: err}
	}()
	close(start)

	for range 3 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent %s mutation failed: %v", result.name, result.err)
			}
		case <-ctx.Done():
			t.Fatalf("metadata/tag/reaction batches did not complete: %v", ctx.Err())
		}
	}

	for _, id := range ids {
		var tags []string
		var source, artist, searchText string
		var metadataVersion, tagVersion, reactionVersion int
		if err := backend.pool.QueryRow(ctx, `
SELECT tags, source, artist, search_text, metadata_version, tag_version, reaction_version
FROM posts WHERE id=$1::bigint`, id).Scan(
			&tags, &source, &artist, &searchText, &metadataVersion, &tagVersion, &reactionVersion,
		); err != nil {
			t.Fatal(err)
		}
		if metadataVersion != 1 || tagVersion != 1 || reactionVersion != 1 {
			t.Fatalf("post %s versions metadata/tag/reaction=%d/%d/%d want 1/1/1", id, metadataVersion, tagVersion, reactionVersion)
		}
		if source != "current source" || artist != "current artist" || !reflect.DeepEqual(tags, []string{"base", "current tag"}) {
			t.Fatalf("post %s current fields source=%q artist=%q tags=%v", id, source, artist, tags)
		}
		if want := canonicalSearchText(tags, source, artist); searchText != want {
			t.Fatalf("post %s search_text=%q want canonical %q", id, searchText, want)
		}
	}
}

func strPtr(value string) *string { return &value }
