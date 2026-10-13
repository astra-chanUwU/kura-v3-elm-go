package posts

import (
	"context"
	"strings"
	"testing"
)

var _ CollectionSummaryReader = (*PostgresBackend)(nil)

func TestCollectionSummariesSQLShape(t *testing.T) {
	sql := listCollectionSummariesSQL
	for _, want := range []string{"COUNT(cp.post_id)", "COUNT(p.id)", "deleted_at IS NULL", "moderation_state = 'published'", "ORDER BY c.created_at, c.id"} {
		if !strings.Contains(sql, want) {
			t.Errorf("summaries SQL missing %q", want)
		}
	}
	if strings.Contains(sql, "array_agg") {
		t.Errorf("summaries SQL materializes member IDs: %s", sql)
	}
	if strings.Count(strings.TrimSpace(sql), ";") != 0 {
		t.Errorf("summaries SQL must be one statement: %s", sql)
	}
	legacy := listCollectionsSQL
	if !strings.Contains(legacy, "ORDER BY c.created_at, c.id") {
		t.Fatalf("legacy ordering changed: %s", legacy)
	}
}

func summaryByID(t *testing.T, summaries []CollectionSummary, id string) CollectionSummary {
	t.Helper()
	for _, s := range summaries {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("summary for %s missing in %#v", id, summaries)
	return CollectionSummary{}
}

func TestCollectionSummariesEmptyAndNonempty(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	empty, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Empty"})
	if err != nil {
		t.Fatal(err)
	}
	ids := seedPublishedPosts(t, ctx, backend, 3)
	full, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Full"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, full.ID, ids, 200)

	summaries, err := backend.ListCollectionSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries=%#v want 2", summaries)
	}
	// Same ordering as legacy list.
	legacy, err := backend.ListCollections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range legacy {
		if summaries[i].ID != legacy[i].ID || summaries[i].Name != legacy[i].Name || summaries[i].Version != legacy[i].Version {
			t.Fatalf("order/metadata diverged at %d: summary %#v legacy %#v", i, summaries[i], legacy[i])
		}
	}
	if got := summaryByID(t, summaries, empty.ID); got.MemberCount != 0 || got.VisiblePostCount != 0 || got.Version != 0 {
		t.Fatalf("empty summary=%#v want counts 0 version 0", got)
	}
	if got := summaryByID(t, summaries, full.ID); got.MemberCount != 3 || got.VisiblePostCount != 3 {
		t.Fatalf("full summary=%#v want 3/3", got)
	}
}

func TestCollectionSummariesHiddenAndDeletedCountedPrecisely(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 6)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Mixed"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, created.ID, ids, 200)
	_, version := collectionVersion(t, ctx, backend, created.ID)

	for _, tc := range []struct {
		id     string
		action string
	}{{ids[1], ModerationActionHide}, {ids[2], ModerationActionReject}, {ids[3], ModerationActionPend}} {
		if _, err := backend.ModeratePosts(ctx, "test", ModerationRequest{Posts: []ModerationTarget{{ID: tc.id}}, Action: tc.action}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE id = $1::bigint`, ids[4]); err != nil {
		t.Fatal(err)
	}

	summaries, err := backend.ListCollectionSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := summaryByID(t, summaries, created.ID)
	// All 6 membership rows count; only ids[0] and ids[5] are published/non-deleted.
	if got.MemberCount != 6 || got.VisiblePostCount != 2 {
		t.Fatalf("summary=%#v want member 6 visible 2", got)
	}
	if got.Version != version {
		t.Fatalf("moderation/delete changed collection version: got %d want %d", got.Version, version)
	}
	// Public browsing agrees with visible_post_count.
	visited, _ := traverseCollection(t, ctx, backend, created.ID, 60)
	if int64(len(visited)) != got.VisiblePostCount {
		t.Fatalf("visible traversal=%d summary visible=%d", len(visited), got.VisiblePostCount)
	}
}

func TestCollectionSummaries1000Members(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 1000)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "HugeSummary"})
	if err != nil {
		t.Fatal(err)
	}
	addCollectionPostsInChunks(t, ctx, backend, created.ID, ids, 200)
	summaries, err := backend.ListCollectionSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := summaryByID(t, summaries, created.ID)
	if got.MemberCount != 1000 || got.VisiblePostCount != 1000 {
		t.Fatalf("summary=%#v want 1000/1000", got)
	}
}

func TestCollectionSummariesMutationsAndMovesVersion(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 6)
	created, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Mutating"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, wantVersion, wantMember, wantVisible int64) {
		t.Helper()
		summaries, err := backend.ListCollectionSummaries(ctx)
		if err != nil {
			t.Fatal(err)
		}
		got := summaryByID(t, summaries, created.ID)
		if got.Version != wantVersion || got.MemberCount != wantMember || got.VisiblePostCount != wantVisible {
			t.Fatalf("%s: summary=%#v want version=%d member=%d visible=%d", name, got, wantVersion, wantMember, wantVisible)
		}
	}
	check("created", 0, 0, 0)
	if _, err := backend.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: ids[:2]}); err != nil {
		t.Fatal(err)
	}
	check("effective add", 1, 2, 2)
	if _, err := backend.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: ids[:2]}); err != nil {
		t.Fatal(err)
	}
	check("duplicate add no-op", 1, 2, 2)
	if _, err := backend.AddCollectionPosts(ctx, created.ID, AddCollectionPostsRequest{PostIDs: []string{ids[1], ids[2]}}); err != nil {
		t.Fatal(err)
	}
	check("mixed add", 2, 3, 3)
	if err := backend.RemoveCollectionPost(ctx, created.ID, ids[5]); err != nil {
		t.Fatal(err)
	}
	check("remove absent no-op", 2, 3, 3)
	if err := backend.RemoveCollectionPost(ctx, created.ID, ids[0]); err != nil {
		t.Fatal(err)
	}
	check("effective remove", 3, 2, 2)
	if _, err := backend.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: []string{ids[1], ids[2]}}); err != nil {
		t.Fatal(err)
	}
	check("reorder same no-op", 3, 2, 2)
	if _, err := backend.ReorderCollection(ctx, created.ID, ReorderCollectionRequest{PostIDs: []string{ids[2], ids[1]}}); err != nil {
		t.Fatal(err)
	}
	check("effective reorder", 4, 2, 2)
	result, err := backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[1], nil, 4))
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("move already-last should be no-op: %#v", result)
	}
	check("move no-op", 4, 2, 2)
	result, err = backend.MoveCollectionPost(ctx, created.ID, moveReq(ids[1], moveStrPtr(ids[2]), 4))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != 5 {
		t.Fatalf("effective move result=%#v", result)
	}
	check("effective move", 5, 2, 2)
	// Hiding does not bump version, drops visible only.
	if _, err := backend.ModeratePosts(ctx, "test", ModerationRequest{Posts: []ModerationTarget{{ID: ids[1]}}, Action: ModerationActionHide}); err != nil {
		t.Fatal(err)
	}
	check("hide member", 5, 2, 1)
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE id = $1::bigint`, ids[2]); err != nil {
		t.Fatal(err)
	}
	check("delete member", 5, 2, 0)
}
