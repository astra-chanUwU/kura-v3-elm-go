package posts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func resetTaxonomy(t *testing.T, ctx context.Context, b *PostgresBackend) {
	t.Helper()
	if _, err := b.pool.Exec(ctx, `DELETE FROM tag_redirects; DELETE FROM tag_operations; UPDATE tag_taxonomy_state SET version=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
}

func taxVersion(t *testing.T, ctx context.Context, b *PostgresBackend) int64 {
	t.Helper()
	var v int64
	if err := b.pool.QueryRow(ctx, `SELECT version FROM tag_taxonomy_state WHERE id=1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func insertTaxPost(t *testing.T, ctx context.Context, b *PostgresBackend, tags []string, source, artist, moderation string, deleted bool) string {
	t.Helper()
	var id string
	err := b.pool.QueryRow(ctx, `INSERT INTO posts (preview_url, original_url, media_type, width, height, source, artist, tags, search_text, moderation_state, deleted_at) VALUES ('/m.png','/m.png','image/png',10,10,$1,$2,$3,$4,$5, CASE WHEN $6 THEN now() END) RETURNING id::text`, source, artist, tags, canonicalSearchText(tags, source, artist), moderation, deleted).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func postTags(t *testing.T, ctx context.Context, b *PostgresBackend, id string) []string {
	t.Helper()
	var tags []string
	var ver int
	if err := b.pool.QueryRow(ctx, `SELECT tags, tag_version FROM posts WHERE id=$1::bigint`, id).Scan(&tags, &ver); err != nil {
		t.Fatal(err)
	}
	_ = ver
	return tags
}

func applyMust(t *testing.T, ctx context.Context, b *PostgresBackend, mode, src, tgt string, expected int64) TagManageResult {
	t.Helper()
	res, err := b.ApplyTagOperation(ctx, "system", TagManageRequest{Mode: mode, SourceTag: src, TargetTag: tgt, ExpectedVersion: &expected})
	if err != nil {
		t.Fatalf("apply %s %s->%s: %v", mode, src, tgt, err)
	}
	return res
}

func TestTagRenamePreservesOrderHiddenTrashHistory(t *testing.T) {
	ctx := context.Background()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	resetTaxonomy(t, ctx, b)
	visible := insertTaxPost(t, ctx, b, []string{"alpha", "old", "beta"}, "s1", "old", "published", false)
	hidden := insertTaxPost(t, ctx, b, []string{"old", "x"}, "", "", "hidden", false)
	trashed := insertTaxPost(t, ctx, b, []string{"old"}, "", "", "published", true)
	// collection membership must be preserved
	var collID int64
	if err := b.pool.QueryRow(ctx, `INSERT INTO collections(name) VALUES('c') RETURNING id`).Scan(&collID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(ctx, `INSERT INTO collection_posts(collection_id,post_id,position) VALUES($1,$2,0)`, collID, visible); err != nil {
		t.Fatal(err)
	}

	preview, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "old", TargetTag: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Version != 0 || preview.AffectedPostCount != 3 || preview.VisiblePostCount != 1 || preview.TargetPostCount != 0 {
		t.Fatalf("preview=%#v", preview)
	}
	res := applyMust(t, ctx, b, "rename", "old", "new", 0)
	if res.Version != 1 || res.AffectedPostCount != 3 || res.VisiblePostCount != 1 || !res.Changed || res.ID == "" {
		t.Fatalf("result=%#v", res)
	}
	if got := postTags(t, ctx, b, visible); !equalTags(got, []string{"alpha", "new", "beta"}) {
		t.Fatalf("visible tags=%v", got)
	}
	if got := postTags(t, ctx, b, hidden); !equalTags(got, []string{"new", "x"}) {
		t.Fatalf("hidden tags=%v", got)
	}
	if got := postTags(t, ctx, b, trashed); !equalTags(got, []string{"new"}) {
		t.Fatalf("trashed tags=%v", got)
	}
	var search, state string
	var deleted bool
	if err := b.pool.QueryRow(ctx, `SELECT search_text, moderation_state, deleted_at IS NOT NULL FROM posts WHERE id=$1::bigint`, visible).Scan(&search, &state, &deleted); err != nil {
		t.Fatal(err)
	}
	if search != "alpha new beta s1 old" {
		t.Fatalf("search=%q artist must not be dropped", search)
	}
	var kind string
	var opID *int64
	var target []string
	if err := b.pool.QueryRow(ctx, `SELECT kind, tag_operation_id, target_tags FROM post_tag_revisions WHERE post_id=$1::bigint AND version=1`, visible).Scan(&kind, &opID, &target); err != nil {
		t.Fatal(err)
	}
	if kind != "rename" || opID == nil || !equalTags(target, []string{"alpha", "new", "beta"}) {
		t.Fatalf("revision kind=%q op=%v target=%v", kind, opID, target)
	}
	var cnt int
	if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM collection_posts WHERE collection_id=$1 AND post_id=$2 AND position=0`, collID, visible).Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("collection preserved cnt=%d err=%v", cnt, err)
	}
	// stale
	if _, err := b.ApplyTagOperation(ctx, "system", TagManageRequest{Mode: "rename", SourceTag: "new", TargetTag: "z", ExpectedVersion: int64p(0)}); !errors.Is(err, ErrTagTaxonomyChanged) {
		t.Fatalf("stale err=%v", err)
	}
	// retired source
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "old", TargetTag: "z"}); !errors.Is(err, ErrTagRetired) {
		t.Fatalf("retired preview err=%v", err)
	}
	// manage includes hidden/deleted counts, public suggestions don't leak hidden-only tags
	page, err := b.ListManageTags(ctx, "NE", 60, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Tags {
		if e.Tag == "new" {
			found = true
			if e.PostCount != 3 || e.VisiblePostCount != 1 {
				t.Fatalf("manage new=%#v", e)
			}
		}
		if e.Tag == "old" {
			t.Fatal("old still in manage")
		}
	}
	if !found || page.Version != 1 {
		t.Fatalf("page=%#v", page)
	}
	sugg, err := b.SuggestTags(ctx, "x", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sugg) != 0 {
		t.Fatalf("hidden tag leaked in suggestions: %#v", sugg)
	}
}

func int64p(v int64) *int64 { return &v }

func TestTagMergeFlattenSearchAndNoResurrection(t *testing.T) {
	ctx := context.Background()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	resetTaxonomy(t, ctx, b)
	p1 := insertTaxPost(t, ctx, b, []string{"mid", "final"}, "", "", "published", false)
	p2 := insertTaxPost(t, ctx, b, []string{"mid"}, "", "", "published", false)
	insertTaxPost(t, ctx, b, []string{"final"}, "", "", "published", false)
	// rename a -> mid chain setup: create tag a on another post then rename a->mid would conflict (mid exists) so chain via rename first
	pA := insertTaxPost(t, ctx, b, []string{"aaa"}, "", "", "published", false)
	applyMust(t, ctx, b, "rename", "aaa", "mid2", 0) // aaa->mid2 v1
	// merge mid2 -> mid (mid exists)
	applyMust(t, ctx, b, "merge", "mid2", "mid", 1) // v2, aaa->mid flattened, mid2->mid
	if got := postTags(t, ctx, b, pA); !equalTags(got, []string{"mid"}) {
		t.Fatalf("pA=%v", got)
	}
	// merge mid -> final
	res := applyMust(t, ctx, b, "merge", "mid", "final", 2)
	if res.Version != 3 {
		t.Fatalf("res=%#v", res)
	}
	if got := postTags(t, ctx, b, p1); !equalTags(got, []string{"final"}) {
		t.Fatalf("merge dedup p1=%v want [final]", got)
	}
	if got := postTags(t, ctx, b, p2); !equalTags(got, []string{"final"}) {
		t.Fatalf("p2=%v", got)
	}
	// redirects flattened
	var aaaCanon, midCanon string
	if err := b.pool.QueryRow(ctx, `SELECT canonical_tag FROM tag_redirects WHERE retired_tag='aaa'`).Scan(&aaaCanon); err != nil {
		t.Fatal(err)
	}
	if err := b.pool.QueryRow(ctx, `SELECT canonical_tag FROM tag_redirects WHERE retired_tag='mid'`).Scan(&midCanon); err != nil {
		t.Fatal(err)
	}
	if aaaCanon != "final" || midCanon != "final" {
		t.Fatalf("flatten aaa=%q mid=%q", aaaCanon, midCanon)
	}
	// search aliases include/exclude
	for _, q := range []string{"tag:aaa", "tag:mid", "tag:mid2", "tag:final"} {
		page, err := b.SearchPosts(ctx, q, nil, 60)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Posts) != 4 {
			t.Fatalf("search %q got %d want 4", q, len(page.Posts))
		}
	}
	page, err := b.SearchPosts(ctx, "-tag:aaa", nil, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 0 {
		t.Fatalf("exclude alias got %d", len(page.Posts))
	}
	// no resurrection
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "merge", SourceTag: "final", TargetTag: "aaa"}); !errors.Is(err, ErrTagRetired) {
		t.Fatalf("resurrect target err=%v", err)
	}
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "aaa", TargetTag: "fresh"}); !errors.Is(err, ErrTagRetired) {
		t.Fatalf("retired source err=%v", err)
	}
	// suggestions: exact retired prefix resolves to canonical
	sugg, err := b.SuggestTags(ctx, "aaa", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sugg) != 1 || sugg[0].Tag != "final" {
		t.Fatalf("alias suggestion=%#v", sugg)
	}
	// EditTags canonicalizes retired add
	other := insertTaxPost(t, ctx, b, []string{"solo"}, "", "", "published", false)
	if _, err := b.EditTags(ctx, TagEditRequest{Posts: []TagTarget{{ID: other, Version: 0}}, Add: []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if got := postTags(t, ctx, b, other); !equalTags(got, []string{"solo", "final"}) {
		t.Fatalf("edit canonicalized=%v", got)
	}
	// canonical overlap rejection: add aaa (=final) and remove mid (=final)
	if _, err := b.EditTags(ctx, TagEditRequest{Posts: []TagTarget{{ID: other, Version: 1}}, Add: []string{"aaa"}, Remove: []string{"mid"}}); !errors.Is(err, ErrInvalidTags) {
		t.Fatalf("overlap err=%v", err)
	}
}

func TestTagPreviewBoundsInvalidAndManageCursor(t *testing.T) {
	ctx := context.Background()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	resetTaxonomy(t, ctx, b)
	if _, err := ValidateTagManageRequest(TagManageRequest{Mode: "rename", SourceTag: "a", TargetTag: "a"}, false); !errors.Is(err, ErrInvalidTagTaxonomy) {
		t.Fatalf("same tag err=%v", err)
	}
	if _, err := ValidateTagManageRequest(TagManageRequest{Mode: "bogus", SourceTag: "a", TargetTag: "b"}, false); !errors.Is(err, ErrInvalidTagTaxonomy) {
		t.Fatalf("mode err=%v", err)
	}
	if _, err := ValidateTagManageRequest(TagManageRequest{Mode: "rename", SourceTag: "a", TargetTag: "b"}, true); !errors.Is(err, ErrInvalidTagTaxonomy) {
		t.Fatalf("missing expected err=%v", err)
	}
	if _, err := ValidateTagManageRequest(TagManageRequest{Mode: "rename", SourceTag: "a", TargetTag: "b", ExpectedVersion: int64p(0)}, true); err != nil {
		t.Fatalf("zero expected valid: %v", err)
	}
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "missing", TargetTag: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing source err=%v", err)
	}
	insertTaxPost(t, ctx, b, []string{"src", "exists"}, "", "", "published", false)
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "src", TargetTag: "exists"}); !errors.Is(err, ErrTagTargetExists) {
		t.Fatalf("rename exists err=%v", err)
	}
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "merge", SourceTag: "src", TargetTag: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("merge missing target err=%v", err)
	}
	// 1001 bound
	if _, err := b.pool.Exec(ctx, `INSERT INTO posts (preview_url, original_url, media_type, width, height, tags) SELECT '/m.png','/m.png','image/png',10,10,ARRAY['bulk'] FROM generate_series(1,1001)`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "bulk", TargetTag: "bulk2"}); !errors.Is(err, ErrTagOperationTooLarge) {
		t.Fatalf("too large preview err=%v", err)
	}
	if _, err := b.ApplyTagOperation(ctx, "system", TagManageRequest{Mode: "rename", SourceTag: "bulk", TargetTag: "bulk2", ExpectedVersion: int64p(0)}); !errors.Is(err, ErrTagOperationTooLarge) {
		t.Fatalf("too large apply err=%v", err)
	}
	// cursor pagination + stale
	resetPostsOnly(t, ctx, b)
	insertTaxPost(t, ctx, b, []string{"a1"}, "", "", "published", false)
	insertTaxPost(t, ctx, b, []string{"a2"}, "", "", "published", false)
	insertTaxPost(t, ctx, b, []string{"b1"}, "", "", "published", false)
	page1, err := b.ListManageTags(ctx, "a", 1, nil)
	if err != nil || len(page1.Tags) != 1 || page1.NextCursor == nil {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	cur, err := DecodeTagManageCursor(*page1.NextCursor, "a")
	if err != nil {
		t.Fatal(err)
	}
	page2, err := b.ListManageTags(ctx, "a", 1, &cur)
	if err != nil || len(page2.Tags) != 1 || page2.Tags[0].Tag != "a2" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	applyMust(t, ctx, b, "rename", "b1", "b2", taxVersion(t, ctx, b))
	if _, err := b.ListManageTags(ctx, "a", 1, &cur); !errors.Is(err, ErrTagTaxonomyChanged) {
		t.Fatalf("stale cursor err=%v", err)
	}
	// literal prefix: % is not wildcard, case-insensitive A matches
	pct, err := b.ListManageTags(ctx, "%", 60, nil)
	if err != nil || len(pct.Tags) != 0 {
		t.Fatalf("pct=%#v err=%v", pct, err)
	}
}

func resetPostsOnly(t *testing.T, ctx context.Context, b *PostgresBackend) {
	t.Helper()
	if _, err := b.pool.Exec(ctx, `TRUNCATE posts CASCADE`); err != nil {
		t.Fatal(err)
	}
	resetTaxonomy(t, ctx, b)
}

func TestTagWritersBumpAndCanonicalizeUploadRevert(t *testing.T) {
	ctx := context.Background()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	resetTaxonomy(t, ctx, b)
	t.Setenv("MEDIA_ROOT", t.TempDir())
	id := insertTaxPost(t, ctx, b, []string{"base"}, "", "", "published", false)
	editTagsMust(t, ctx, b, id, 0, []string{"old"}, nil) // v1 taxonomy bump
	if v := taxVersion(t, ctx, b); v != 1 {
		t.Fatalf("after edit version=%d want1", v)
	}
	// no-op edit does not bump
	editTagsMust(t, ctx, b, id, 1, []string{"old"}, nil)
	if v := taxVersion(t, ctx, b); v != 1 {
		t.Fatalf("noop edit bumped to %d", v)
	}
	applyMust(t, ctx, b, "rename", "old", "fresh", 1) // v2, post v2
	// revert to v1 snapshot [base old] canonicalizes to [base fresh]
	res := revertTagsMust(t, ctx, b, id, 2, 1)
	if !equalTags(res.Tags, []string{"base", "fresh"}) {
		t.Fatalf("revert canonical=%v", res.Tags)
	}
	// historic snapshot verbatim
	var hist []string
	if err := b.pool.QueryRow(ctx, `SELECT target_tags FROM post_tag_revisions WHERE post_id=$1::bigint AND version=1`, id).Scan(&hist); err != nil {
		t.Fatal(err)
	}
	if !equalTags(hist, []string{"base", "old"}) {
		t.Fatalf("historic mutated=%v", hist)
	}
	// stale taxonomy after regular edit: preview then edit bumps, apply fails
	preview, err := b.PreviewTagOperation(ctx, TagManageRequest{Mode: "rename", SourceTag: "fresh", TargetTag: "newer"})
	if err != nil {
		t.Fatal(err)
	}
	other := insertTaxPost(t, ctx, b, []string{"z"}, "", "", "published", false)
	editTagsMust(t, ctx, b, other, 0, []string{"y"}, nil)
	if _, err := b.ApplyTagOperation(ctx, "system", TagManageRequest{Mode: "rename", SourceTag: "fresh", TargetTag: "newer", ExpectedVersion: int64p(preview.Version)}); !errors.Is(err, ErrTagTaxonomyChanged) {
		t.Fatalf("apply after edit err=%v", err)
	}
	// upload with retired tag canonicalizes and bumps
	before := taxVersion(t, ctx, b)
	detail, err := b.CreateUpload(ctx, UploadRequest{File: bytes.NewReader(testPNG(t, 4, 4)), Filename: "u.png", Tags: []string{"old", "fresh"}})
	if err != nil {
		t.Fatal(err)
	}
	if !equalTags(detail.Tags, []string{"fresh"}) {
		t.Fatalf("upload tags=%v want [fresh] deduped canonical", detail.Tags)
	}
	if taxVersion(t, ctx, b) != before+1 {
		t.Fatalf("upload bump before=%d now=%d", before, taxVersion(t, ctx, b))
	}
	// invalid canonical post ID rejected before lookup
	if _, err := ValidateTagEdit(TagEditRequest{Posts: []TagTarget{{ID: " 1", Version: 0}}, Add: []string{"x"}}); !errors.Is(err, ErrInvalidTags) {
		t.Fatalf("whitespace id err=%v", err)
	}
}

func TestBrowserImportCanonicalizesAndDuplicateDoesNotBump(t *testing.T) {
	ctx := context.Background()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	resetTaxonomy(t, ctx, b)
	t.Setenv("MEDIA_ROOT", t.TempDir())
	insertTaxPost(t, ctx, b, []string{"old"}, "", "", "published", false)
	applyMust(t, ctx, b, "rename", "old", "fresh", 0)
	before := taxVersion(t, ctx, b)
	data := testPNG(t, 6, 4)
	req := browserImportRequest(data, "imp.png")
	req.Tags = []string{"old"}
	res, err := b.ImportBrowser(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate || !equalTags(res.Post.Tags, []string{"fresh"}) {
		t.Fatalf("import=%#v tags=%v", res.Duplicate, res.Post.Tags)
	}
	if taxVersion(t, ctx, b) != before+1 {
		t.Fatalf("import bump before=%d now=%d", before, taxVersion(t, ctx, b))
	}
	req2 := browserImportRequest(data, "imp2.png")
	req2.Tags = []string{"old"}
	dup, err := b.ImportBrowser(ctx, req2)
	if err != nil {
		t.Fatal(err)
	}
	if !dup.Duplicate {
		t.Fatalf("expected duplicate: %#v", dup)
	}
	if taxVersion(t, ctx, b) != before+1 {
		t.Fatalf("duplicate bumped to %d", taxVersion(t, ctx, b))
	}
}

func TestConcurrentAppliesOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	resetTaxonomy(t, ctx, b)
	insertTaxPost(t, ctx, b, []string{"conc"}, "", "", "published", false)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = b.ApplyTagOperation(ctx, "system", TagManageRequest{Mode: "rename", SourceTag: "conc", TargetTag: "done", ExpectedVersion: int64p(0)})
		}(i)
	}
	close(start)
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrTagTaxonomyChanged) && !errors.Is(err, ErrTagRetired) {
			t.Fatalf("concurrent err=%v", err)
		}
	}
	if success != 1 || taxVersion(t, ctx, b) != 1 {
		t.Fatalf("success=%d version=%d errs=%v", success, taxVersion(t, ctx, b), errs)
	}
}

func TestTagManageCursorRejectsInvalidText(t *testing.T) {
	for _, last := range []string{"x\x00", " x", strings.Repeat("x", 129), ""} {
		payload, err := json.Marshal(tagManageCursorPayload{V: 1, Prefix: "", Version: 0, Last: last})
		if err != nil {
			t.Fatal(err)
		}
		token := base64.RawURLEncoding.EncodeToString(payload)
		if _, err := DecodeTagManageCursor(token, ""); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("last=%q: %v", last, err)
		}
		if _, err := EncodeTagManageCursor("", 0, last); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("encode last=%q: %v", last, err)
		}
	}
}

func TestTagOperationAndOrdinaryEditSerialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := newTagRevertTestBackend(t, ctx, tagRevertTestDatabaseURL(t))
	id := insertTaxPost(t, ctx, b, []string{"old"}, "", "", "published", false)
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		_, err := b.ApplyTagOperation(ctx, "system", TagManageRequest{Mode: "rename", SourceTag: "old", TargetTag: "fresh", ExpectedVersion: int64p(0)})
		errs <- err
	}()
	go func() {
		<-start
		_, err := b.EditTags(ctx, TagEditRequest{Posts: []TagTarget{{ID: id, Version: 0}}, Add: []string{"extra"}})
		errs <- err
	}()
	close(start)
	winners := 0
	for i := 0; i < 2; i++ {
		err := <-errs
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrTagTaxonomyChanged) {
			t.Fatalf("concurrent mutation: %v", err)
		}
	}
	if winners != 1 || taxVersion(t, ctx, b) != 1 {
		t.Fatalf("winners=%d taxonomy=%d", winners, taxVersion(t, ctx, b))
	}
	tags := postTags(t, ctx, b, id)
	if !equalTags(tags, []string{"fresh"}) && !equalTags(tags, []string{"old", "extra"}) {
		t.Fatalf("non-atomic tags: %v", tags)
	}
}
