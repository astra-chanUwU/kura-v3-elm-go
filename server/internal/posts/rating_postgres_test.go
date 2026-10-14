package posts

import (
	"context"
	"errors"
	"testing"
)

func TestPostgresRatingsAtomicHistoryAndVisibility(t *testing.T) {
	ctx := context.Background()
	backend := newMetadataTestBackend(t, ctx, metadataTestDatabaseURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 2)

	for _, id := range ids {
		var rating string
		var version int
		if err := backend.pool.QueryRow(ctx, `SELECT rating, rating_version FROM posts WHERE id=$1::bigint`, id).Scan(&rating, &version); err != nil {
			t.Fatal(err)
		}
		if rating != "unrated" || version != 0 {
			t.Fatalf("post %s defaults = %q v%d, want unrated v0", id, rating, version)
		}
		for _, allowed := range []string{"unrated", "safe", "questionable", "explicit"} {
			if _, err := backend.pool.Exec(ctx, `UPDATE posts SET rating=$1 WHERE id=$2::bigint`, allowed, id); err != nil {
				t.Errorf("database rejected allowed rating %q: %v", allowed, err)
			}
		}
		if _, err := backend.pool.Exec(ctx, `UPDATE posts SET rating='unrated' WHERE id=$1::bigint`, id); err != nil {
			t.Fatal(err)
		}
		for _, invalid := range []string{"SAFE", "unknown", ""} {
			if _, err := backend.pool.Exec(ctx, `UPDATE posts SET rating=$1 WHERE id=$2::bigint`, invalid, id); err == nil {
				t.Errorf("database accepted invalid rating %q", invalid)
			}
		}
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET rating_version=-1 WHERE id=$1::bigint`, ids[0]); err == nil {
		t.Fatal("database accepted negative rating version")
	}

	request := RatingRequest{
		Posts:  []RatingEdit{{ID: ids[0], Version: intPointer(0)}, {ID: ids[1], Version: intPointer(1)}},
		Rating: RatingSafe,
	}
	if _, err := backend.UpdateRatings(ctx, request); !errors.Is(err, ErrRatingConflict) {
		t.Fatalf("stale batch err=%v want ErrRatingConflict", err)
	}
	var unchanged int
	if err := backend.pool.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE id=ANY($1::bigint[]) AND rating='unrated' AND rating_version=0`, ids).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != 2 {
		t.Fatalf("stale batch changed %d posts", 2-unchanged)
	}

	request.Posts[1].Version = intPointer(0)
	result, err := backend.UpdateRatings(ctx, request)
	if err != nil || len(result.Posts) != 2 || !result.Posts[0].Changed || !result.Posts[1].Changed || result.Posts[0].Version != 1 {
		t.Fatalf("rating edit result=%#v err=%v", result, err)
	}
	if _, err := backend.UpdateRatings(ctx, RatingRequest{Posts: []RatingEdit{{ID: ids[0], Version: intPointer(0)}}, Rating: RatingSafe}); !errors.Is(err, ErrRatingConflict) {
		t.Fatalf("stale same-value edit err=%v want conflict", err)
	}
	noop, err := backend.UpdateRatings(ctx, RatingRequest{Posts: []RatingEdit{{ID: ids[0], Version: intPointer(1)}}, Rating: RatingSafe})
	if err != nil || noop.Posts[0].Changed || noop.Posts[0].Version != 1 {
		t.Fatalf("same-value edit should be no-op: %#v err=%v", noop, err)
	}
	var revisions int
	if err := backend.pool.QueryRow(ctx, `SELECT COUNT(*) FROM post_rating_revisions WHERE post_id=$1::bigint`, ids[0]).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 1 {
		t.Fatalf("revision count=%d want 1", revisions)
	}
	detail, err := backend.GetPostDetail(ctx, ids[0])
	if err != nil || detail.Rating != RatingSafe || detail.RatingVersion != 1 || len(detail.RatingHistory) != 1 || detail.RatingHistory[0].PreviousRating != RatingUnrated {
		t.Fatalf("detail rating history=%#v err=%v", detail, err)
	}

	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET moderation_state='hidden' WHERE id=$1::bigint`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.UpdateRatings(ctx, RatingRequest{Posts: []RatingEdit{{ID: ids[0], Version: intPointer(1)}}, Rating: RatingExplicit}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden post edit err=%v want ErrNotFound", err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at=now() WHERE id=$1::bigint`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.UpdateRatings(ctx, RatingRequest{Posts: []RatingEdit{{ID: ids[1], Version: intPointer(1)}}, Rating: RatingExplicit}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted post edit err=%v want ErrNotFound", err)
	}
}

func TestPostgresRatingSearchAndCollectionSummaries(t *testing.T) {
	ctx := context.Background()
	backend := newMetadataTestBackend(t, ctx, metadataTestDatabaseURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 3)
	for i, rating := range []Rating{RatingSafe, RatingSafe, RatingExplicit} {
		if _, err := backend.UpdateRatings(ctx, RatingRequest{Posts: []RatingEdit{{ID: ids[i], Version: intPointer(0)}}, Rating: rating}); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.pool.Exec(ctx, `UPDATE posts SET score=$1 WHERE id=$2::bigint`, 3-i, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	query := "rating:safe order:score"
	page, err := backend.SearchPosts(ctx, query, nil, 1)
	if err != nil || len(page.Posts) != 1 || page.NextCursor == nil || page.Posts[0].Rating != RatingSafe || page.Posts[0].RatingVersion != 1 {
		t.Fatalf("first search page=%#v err=%v", page, err)
	}
	cursor, err := DecodeCursor(*page.NextCursor, query)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCursor(*page.NextCursor, "rating:explicit order:score"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("rating cursor accepted a different query: %v", err)
	}
	second, err := backend.SearchPosts(ctx, query, &cursor, 1)
	if err != nil || len(second.Posts) != 1 || second.Posts[0].ID == page.Posts[0].ID || second.Posts[0].Rating != RatingSafe {
		t.Fatalf("second search page=%#v err=%v", second, err)
	}
	newestQuery := "rating:safe"
	newestFirst, err := backend.SearchPosts(ctx, newestQuery, nil, 1)
	if err != nil || len(newestFirst.Posts) != 1 || newestFirst.NextCursor == nil {
		t.Fatalf("newest first page=%#v err=%v", newestFirst, err)
	}
	newestCursor, err := DecodeCursor(*newestFirst.NextCursor, newestQuery)
	if err != nil {
		t.Fatal(err)
	}
	newestSecond, err := backend.SearchPosts(ctx, newestQuery, &newestCursor, 1)
	if err != nil || len(newestSecond.Posts) != 1 || newestSecond.Posts[0].ID == newestFirst.Posts[0].ID || newestSecond.Posts[0].Rating != RatingSafe {
		t.Fatalf("newest second page=%#v err=%v", newestSecond, err)
	}

	collection, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "ratings"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AddCollectionPosts(ctx, collection.ID, AddCollectionPostsRequest{PostIDs: []string{ids[0]}}); err != nil {
		t.Fatal(err)
	}
	collectionPage, err := backend.SearchCollectionPosts(ctx, collection.ID, nil, 10)
	if err != nil || len(collectionPage.Posts) != 1 || collectionPage.Posts[0].Rating != RatingSafe || collectionPage.Posts[0].RatingVersion != 1 {
		t.Fatalf("collection page=%#v err=%v", collectionPage, err)
	}
}

func TestPostgresRatingCursorsUseNumericPostIDs(t *testing.T) {
	ctx := context.Background()
	backend := newMetadataTestBackend(t, ctx, metadataTestDatabaseURL(t))
	ids := []string{"9", "10", "99", "100"}
	for _, id := range ids {
		if _, err := backend.pool.Exec(ctx, `
INSERT INTO posts (id, preview_url, original_url, media_type, width, height, rating, rating_version)
VALUES ($1::bigint, '/preview/' || $1::text, '/original/' || $1::text, 'image/jpeg', 10, 10, 'safe', 1)`, id); err != nil {
			t.Fatalf("insert fixture post %s: %v", id, err)
		}
	}
	if _, err := backend.pool.Exec(ctx, `
INSERT INTO posts (id, preview_url, original_url, media_type, width, height, rating)
VALUES (2, '/preview/2', '/original/2', 'image/jpeg', 10, 10, 'unrated')`); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"rating:safe", "rating:safe order:score"} {
		var got []string
		var cursor *Cursor
		for {
			page, err := backend.SearchPosts(ctx, query, cursor, 2)
			if err != nil {
				t.Fatalf("search %q: %v", query, err)
			}
			for _, post := range page.Posts {
				got = append(got, post.ID)
			}
			if page.NextCursor == nil {
				break
			}
			decoded, err := DecodeCursor(*page.NextCursor, query)
			if err != nil {
				t.Fatalf("decode %q cursor: %v", query, err)
			}
			cursor = &decoded
		}
		if len(got) != 4 || got[0] != "100" || got[1] != "99" || got[2] != "10" || got[3] != "9" {
			t.Errorf("search %q IDs=%v; want numeric descending [100 99 10 9]", query, got)
		}
	}

	// The lock queries return numeric ID order even when the caller submits
	// targets in the opposite order, matching tag/reaction lock acquisition.
	rows, err := backend.pool.Query(ctx, lockPostsForRatingSQL, []int64{10, 2})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("rating lock query returned no fixtures")
	}
	var firstID string
	var rating Rating
	var version int
	if err := rows.Scan(&firstID, &rating, &version); err != nil {
		t.Fatal(err)
	}
	if firstID != "2" || !rows.Next() {
		t.Fatalf("rating lock query did not order numerically: first=%s", firstID)
	}
	var secondID string
	if err := rows.Scan(&secondID, &rating, &version); err != nil {
		t.Fatal(err)
	}
	if secondID != "10" || rows.Next() {
		t.Fatalf("rating lock query order is [%s %s], want [2 10]", firstID, secondID)
	}

	metadataRows, err := backend.pool.Query(ctx, lockPostsForMetadataSQL, []int64{10, 2})
	if err != nil {
		t.Fatal(err)
	}
	defer metadataRows.Close()
	var metadataIDs []string
	for metadataRows.Next() {
		var id, source, artist string
		var tags []string
		var metadataVersion int
		if err := metadataRows.Scan(&id, &source, &artist, &tags, &metadataVersion); err != nil {
			t.Fatal(err)
		}
		metadataIDs = append(metadataIDs, id)
	}
	if err := metadataRows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(metadataIDs) != 2 || metadataIDs[0] != "2" || metadataIDs[1] != "10" {
		t.Fatalf("metadata lock query order=%v, want [2 10]", metadataIDs)
	}

	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET moderation_state='pending' WHERE id=ANY($1::bigint[])`, []int64{9, 10, 99, 100}); err != nil {
		t.Fatal(err)
	}
	queue, err := backend.ListModerationQueue(ctx, "pending", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Posts) != 4 || queue.Posts[0].ID != "100" || queue.Posts[1].ID != "99" || queue.Posts[2].ID != "10" || queue.Posts[3].ID != "9" {
		t.Fatalf("moderation queue IDs are not numeric descending: %#v", queue.Posts)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at='2026-10-01T00:00:00Z' WHERE id=ANY($1::bigint[])`, []int64{9, 10, 99, 100}); err != nil {
		t.Fatal(err)
	}
	trash, err := backend.ListTrash(ctx, "ratings-test", 2, nil)
	if err != nil || len(trash.Posts) != 2 || trash.NextCursor == nil || trash.Posts[0].ID != "100" || trash.Posts[1].ID != "99" {
		t.Fatalf("trash first page=%#v err=%v", trash, err)
	}
	trashCursor, err := DecodeTrashCursor(*trash.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	trashNext, err := backend.ListTrash(ctx, "ratings-test", 2, &trashCursor)
	if err != nil || len(trashNext.Posts) != 2 || trashNext.Posts[0].ID != "10" || trashNext.Posts[1].ID != "9" {
		t.Fatalf("trash second page=%#v err=%v", trashNext, err)
	}

}
