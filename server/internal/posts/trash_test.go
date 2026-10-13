package posts

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTrashValidationAndCursor(t *testing.T) {
	for _, request := range []TrashRequest{
		{},
		{PostIDs: []string{"01"}},
		{PostIDs: []string{"1", "1"}},
		{PostIDs: []string{"9223372036854775808"}},
		{PostIDs: []string{"1"}, Reason: string(make([]byte, maxTrashReasonLength+1))},
	} {
		if _, err := ValidateTrashRequest(request); !errors.Is(err, ErrInvalidTrash) {
			t.Errorf("ValidateTrashRequest(%#v) err=%v", request, err)
		}
	}
	valid, err := ValidateTrashRequest(TrashRequest{PostIDs: []string{" 7 ", "2"}, Reason: "  cleanup  "})
	if err != nil || valid.PostIDs[0] != "7" || valid.Reason != "cleanup" {
		t.Fatalf("normalized request=%#v err=%v", valid, err)
	}
	stamp := time.Date(2026, 10, 12, 12, 30, 5, 123456000, time.UTC)
	token, err := EncodeTrashCursor(stamp, 91)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := DecodeTrashCursor(token)
	if err != nil || !cursor.DeletedAt.Equal(stamp) || cursor.ID != 91 {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
	for _, malformed := range []string{"", "%%%", "e30", token + "x"} {
		if _, err := DecodeTrashCursor(malformed); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("DecodeTrashCursor(%q) err=%v", malformed, err)
		}
	}
}

func TestPostTrashConcurrentCollectionAdd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := collectionPaginationTestURL(t)
	backend := newCollectionPaginationBackend(t, ctx, url)
	ids := seedPublishedPosts(t, ctx, backend, 12)
	collection, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "trash add race"})
	if err != nil {
		t.Fatal(err)
	}
	for index, postID := range ids {
		var before int64
		if err := backend.pool.QueryRow(ctx, `SELECT version FROM collections WHERE id = $1::bigint`, collection.ID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var ready sync.WaitGroup
		ready.Add(2)
		go func() {
			ready.Done()
			<-start
			_, err := backend.AddCollectionPosts(ctx, collection.ID, AddCollectionPostsRequest{PostIDs: []string{postID}})
			results <- err
		}()
		go func() {
			ready.Done()
			<-start
			_, err := backend.Trash(ctx, "owner-race", TrashRequest{PostIDs: []string{postID}, Reason: fmt.Sprintf("race %d", index)})
			results <- err
		}()
		ready.Wait()
		close(start)
		for range 2 {
			if err := <-results; err != nil {
				t.Fatalf("racing add/trash for post %s: %v", postID, err)
			}
		}
		var memberCount int
		var after int64
		if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM collection_posts WHERE collection_id = $1::bigint AND post_id = $2::bigint`, collection.ID, postID).Scan(&memberCount); err != nil {
			t.Fatal(err)
		}
		if err := backend.pool.QueryRow(ctx, `SELECT version FROM collections WHERE id = $1::bigint`, collection.ID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		switch {
		case memberCount == 0 && after == before:
			// Trash serialized first; the later add saw deleted_at and skipped it.
		case memberCount == 1 && after == before+2:
			// Add serialized first; trash kept membership and invalidated the page.
		default:
			t.Fatalf("post %s race result members=%d version=%d before=%d", postID, memberCount, after, before)
		}
	}
}

func TestPostTrashPostgresLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := collectionPaginationTestURL(t)
	backend := newCollectionPaginationBackend(t, ctx, url)
	ids := seedPublishedPosts(t, ctx, backend, 2)
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET tags = '{cat}', favorite = true, score = 4, source = 'kept' WHERE id = $1::bigint`, ids[0]); err != nil {
		t.Fatal(err)
	}
	collection, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "trash retention"})
	if err != nil {
		t.Fatal(err)
	}
	collection, err = backend.AddCollectionPosts(ctx, collection.ID, AddCollectionPostsRequest{PostIDs: []string{ids[0]}})
	if err != nil || collection.Version != 1 {
		t.Fatalf("add member collection=%#v err=%v", collection, err)
	}

	first, err := backend.Trash(ctx, "owner-a", TrashRequest{PostIDs: []string{ids[0]}, Reason: "cleanup"})
	if err != nil || len(first.Posts) != 1 || !first.Posts[0].Changed {
		t.Fatalf("trash response=%#v err=%v", first, err)
	}
	repeated, err := backend.Trash(ctx, "owner-a", TrashRequest{PostIDs: []string{ids[0]}})
	if err != nil || repeated.Posts[0].Changed {
		t.Fatalf("repeated trash response=%#v err=%v", repeated, err)
	}
	var version int64
	if err := backend.pool.QueryRow(ctx, `SELECT version FROM collections WHERE id = $1::bigint`, collection.ID).Scan(&version); err != nil || version != 2 {
		t.Fatalf("trash version=%d err=%v want 2", version, err)
	}
	var members int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM collection_posts WHERE collection_id = $1::bigint AND post_id = $2::bigint`, collection.ID, ids[0]).Scan(&members); err != nil || members != 1 {
		t.Fatalf("membership count=%d err=%v", members, err)
	}
	var state, source string
	var tags []string
	var favorite bool
	var score int
	if err := backend.pool.QueryRow(ctx, `SELECT moderation_state, source, tags, favorite, score FROM posts WHERE id = $1::bigint`, ids[0]).Scan(&state, &source, &tags, &favorite, &score); err != nil {
		t.Fatal(err)
	}
	if state != ModerationPublished || source != "kept" || len(tags) != 1 || tags[0] != "cat" || !favorite || score != 4 {
		t.Fatalf("post metadata changed: state=%q source=%q tags=%v favorite=%v score=%d", state, source, tags, favorite, score)
	}

	restored, err := backend.Restore(ctx, "owner-a", TrashRequest{PostIDs: []string{ids[0]}})
	if err != nil || !restored.Posts[0].Changed {
		t.Fatalf("restore response=%#v err=%v", restored, err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT version FROM collections WHERE id = $1::bigint`, collection.ID).Scan(&version); err != nil || version != 3 {
		t.Fatalf("restore version=%d err=%v want 3", version, err)
	}
	if _, err := backend.Trash(ctx, "owner-a", TrashRequest{PostIDs: []string{ids[0], ids[1]}}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = CASE id WHEN $1::bigint THEN now() - interval '1 minute' ELSE now() END WHERE id = ANY($2::bigint[])`, ids[0], []int64{parsePostID(ids[0]), parsePostID(ids[1])}); err != nil {
		t.Fatal(err)
	}
	page, err := backend.ListTrash(ctx, "owner-a", 1, nil)
	if err != nil || len(page.Posts) != 1 || page.NextCursor == nil || page.Posts[0].ID != ids[1] {
		t.Fatalf("first trash page=%#v err=%v", page, err)
	}
	cursor, err := DecodeTrashCursor(*page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	next, err := backend.ListTrash(ctx, "owner-a", 1, &cursor)
	if err != nil || len(next.Posts) != 1 || next.Posts[0].ID != ids[0] || next.NextCursor != nil {
		t.Fatalf("next trash page=%#v err=%v", next, err)
	}
	var auditCount int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM post_trash_actions WHERE post_id = $1::bigint`, ids[0]).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("audit rows=%d err=%v want three effective changes", auditCount, err)
	}
}
