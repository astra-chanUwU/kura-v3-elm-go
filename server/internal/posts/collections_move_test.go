package posts

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func moveStrPtr(s string) *string { return &s }

func TestValidateMoveCollectionPostRequest(t *testing.T) {
	before := "12"
	got, err := ValidateMoveCollectionPostRequest(MoveCollectionPostRequest{PostID: "10", BeforePostID: &before, ExpectedVersion: 3})
	if err != nil || got.PostID != "10" || got.BeforePostID == nil || *got.BeforePostID != "12" || got.ExpectedVersion != 3 {
		t.Fatalf("valid move rejected: %#v, %v", got, err)
	}
	got, err = ValidateMoveCollectionPostRequest(MoveCollectionPostRequest{PostID: "10", ExpectedVersion: 0})
	if err != nil || got.BeforePostID != nil {
		t.Fatalf("move to end rejected: %#v, %v", got, err)
	}
	for _, req := range []MoveCollectionPostRequest{
		{PostID: "0", ExpectedVersion: 0},
		{PostID: "01", ExpectedVersion: 0},
		{PostID: "bad", ExpectedVersion: 0},
		{PostID: "", ExpectedVersion: 0},
		{PostID: "10", BeforePostID: moveStrPtr("0"), ExpectedVersion: 0},
		{PostID: "10", BeforePostID: moveStrPtr("01"), ExpectedVersion: 0},
		{PostID: "10", BeforePostID: moveStrPtr(""), ExpectedVersion: 0},
		{PostID: "10", ExpectedVersion: -1},
	} {
		if _, err := ValidateMoveCollectionPostRequest(req); !errors.Is(err, ErrInvalidCollections) {
			t.Errorf("request %#v: err=%v want ErrInvalidCollections", req, err)
		}
	}
}

func TestMoveCollectionOrder(t *testing.T) {
	existing := []int64{10, 11, 12, 13, 14}
	cases := []struct {
		name    string
		post    int64
		before  *int64
		want    []int64
		changed bool
	}{
		{"first to end", 10, nil, []int64{11, 12, 13, 14, 10}, true},
		{"last before first", 14, moveIntPtr(10), []int64{14, 10, 11, 12, 13}, true},
		{"middle before neighbor", 11, moveIntPtr(14), []int64{10, 12, 13, 11, 14}, true},
		{"cross page forward", 10, moveIntPtr(13), []int64{11, 12, 10, 13, 14}, true},
		{"self move", 12, moveIntPtr(12), []int64{10, 11, 12, 13, 14}, false},
		{"already before neighbor", 11, moveIntPtr(12), []int64{10, 11, 12, 13, 14}, false},
		{"already last to end", 14, nil, []int64{10, 11, 12, 13, 14}, false},
		{"already first before second", 10, moveIntPtr(11), []int64{10, 11, 12, 13, 14}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed, err := moveCollectionOrder(existing, tc.post, tc.before)
			if err != nil {
				t.Fatal(err)
			}
			if changed != tc.changed {
				t.Fatalf("changed=%v want %v (got %v)", changed, tc.changed, got)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
	if _, _, err := moveCollectionOrder(existing, 99, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing post err=%v want ErrNotFound", err)
	}
	if _, _, err := moveCollectionOrder(existing, 10, moveIntPtr(99)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing neighbor err=%v want ErrNotFound", err)
	}
	// 1,000-member order computation has no size cap.
	large := make([]int64, 1000)
	for i := range large {
		large[i] = int64(i + 1)
	}
	got, changed, err := moveCollectionOrder(large, 1, nil)
	if err != nil || !changed || got[999] != 1 || got[0] != 2 {
		t.Fatalf("1000 move: changed=%v err=%v first=%d last=%d", changed, err, got[0], got[999])
	}
}

func moveIntPtr(v int64) *int64 { return &v }

func allMemberIDs(t *testing.T, ctx context.Context, backend *PostgresBackend, collectionID string) []string {
	t.Helper()
	rows, err := backend.pool.Query(ctx, `SELECT post_id::text FROM collection_posts WHERE collection_id=$1::bigint ORDER BY position, post_id`, collectionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
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
	return ids
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order len=%d want %d: got %v want %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d got %q want %q (got %v want %v)", i, got[i], want[i], got, want)
		}
	}
}

func moveReq(postID string, before *string, version int64) MoveCollectionPostRequest {
	return MoveCollectionPostRequest{PostID: postID, BeforePostID: before, ExpectedVersion: version}
}

func TestCollectionMovePostgres(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 8)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Move"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, created.ID, ids, 200)
	_, version := collectionVersion(t, ctx, backend, created.ID)

	// First to end.
	result, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], nil, version))
	if err != nil {
		t.Fatal(err)
	}
	if result.CollectionID != created.ID || result.ID != created.ID || !result.Changed || result.Version != version+1 {
		t.Fatalf("first-to-end result: %#v", result)
	}
	version = result.Version
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), []string{ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7], ids[0]})

	// Last before first.
	result, err = backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], moveStrPtr(ids[1]), version))
	if err != nil {
		t.Fatal(err)
	}
	version = result.Version
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), ids)

	// Cross-page style move: move a later member ahead of an early one.
	result, err = backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[6], moveStrPtr(ids[1]), version))
	if err != nil {
		t.Fatal(err)
	}
	version = result.Version
	want := []string{ids[0], ids[6], ids[1], ids[2], ids[3], ids[4], ids[5], ids[7]}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), want)
	// Visible traversal follows the same order.
	visited, _ := traverseCollection(t, ctx, backend, created.ID, 3)
	assertTraversal(t, visited, want)

	// No-ops: self-move, already-correct placement, already-last to end.
	for _, req := range []MoveCollectionPostRequest{
		moveReq(ids[6], moveStrPtr(ids[6]), version),
		moveReq(ids[6], moveStrPtr(ids[1]), version),
		moveReq(ids[7], nil, version),
	} {
		result, err = backend.MoveCollectionPost(ctx, created.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		if result.Changed || result.Version != version {
			t.Fatalf("no-op move %#v: result %#v want unchanged version %d", req, result, version)
		}
	}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), want)

	// Stale version is rejected and leaves order untouched.
	if _, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], nil, version-1)); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("stale move err=%v want ErrCollectionChanged", err)
	}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), want)

	// Invalid neighbors / members / collection.
	extra := seedPublishedPosts(t, ctx, backend, 1)
	if _, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], moveStrPtr(extra[0]), version)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member neighbor err=%v want ErrNotFound", err)
	}
	if _, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(extra[0], nil, version)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member post err=%v want ErrNotFound", err)
	}
	if _, err := backend.MoveCollectionPost(ctx, "999999999", moveReq(ids[0], nil, 0)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing collection err=%v want ErrNotFound", err)
	}
	if _, err := backend.MoveCollectionPost(ctx, "bad", moveReq(ids[0], nil, version)); !errors.Is(err, ErrInvalidCollections) {
		t.Fatalf("bad collection id err=%v want ErrInvalidCollections", err)
	}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), want)
	if _, current := collectionVersion(t, ctx, backend, created.ID); current != version {
		t.Fatalf("failed moves changed version to %d want %d", current, version)
	}
}

func TestCollectionMovePreservesHiddenAndDeleted(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 5)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "HiddenMove"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, created.ID, ids, 200)
	if _, err := backend.ModeratePosts(ctx, "test", ModerationRequest{Posts: []ModerationTarget{{ID: ids[1]}}, Action: ModerationActionHide}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE id = $1::bigint`, ids[3]); err != nil {
		t.Fatal(err)
	}
	_, version := collectionVersion(t, ctx, backend, created.ID)

	// Move the visible first member to the end: hidden/deleted keep places.
	result, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], nil, version))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatalf("move result: %#v", result)
	}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), []string{ids[1], ids[2], ids[3], ids[4], ids[0]})
	// Visible page hides hidden/deleted but does not lose them in storage.
	visited, _ := traverseCollection(t, ctx, backend, created.ID, 60)
	assertTraversal(t, visited, []string{ids[2], ids[4], ids[0]})

	// Moving relative to a hidden neighbor works and stays deterministic.
	result, err = backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], moveStrPtr(ids[1]), result.Version))
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), []string{ids[0], ids[1], ids[2], ids[3], ids[4]})
	visited, _ = traverseCollection(t, ctx, backend, created.ID, 60)
	assertTraversal(t, visited, []string{ids[0], ids[2], ids[4]})
}

func TestCollectionMoveConcurrentStale(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 10)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "MoveRace"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, created.ID, ids, 200)
	_, version := collectionVersion(t, ctx, backend, created.ID)

	// Two moves with the same expected version race: exactly one wins.
	var group sync.WaitGroup
	results := make(chan error, 2)
	group.Add(2)
	go func() {
		defer group.Done()
		_, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[0], nil, version))
		results <- err
	}()
	go func() {
		defer group.Done()
		_, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[9], moveStrPtr(ids[0]), version))
		results <- err
	}()
	group.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrCollectionChanged):
			conflicted++
		default:
			t.Fatalf("concurrent move: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent moves succeeded=%d conflicted=%d want 1/1", succeeded, conflicted)
	}
	if _, current := collectionVersion(t, ctx, backend, created.ID); current != version+1 {
		t.Fatalf("version=%d want %d", current, version+1)
	}
	if got := allMemberIDs(t, ctx, backend, created.ID); len(got) != 10 {
		t.Fatalf("members lost in race: %v", got)
	}
}

func TestCollectionMove1000Members(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 1000)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "MoveHuge"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, created.ID, ids, 200)
	_, version := collectionVersion(t, ctx, backend, created.ID)

	result, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[999], moveStrPtr(ids[0]), version))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != version+1 {
		t.Fatalf("1000 move result: %#v", result)
	}
	want := append([]string{ids[999]}, ids[:999]...)
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), want)
	visited, _ := traverseCollection(t, ctx, backend, created.ID, 60)
	assertTraversal(t, visited, want)

	// And back to the end with the fresh version.
	result, err = backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[999], nil, result.Version))
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, allMemberIDs(t, ctx, backend, created.ID), ids)
}
