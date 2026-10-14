package posts

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestMetadataReadValidation(t *testing.T) {
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint(i + 1)
	}
	for _, ids := range [][]string{nil, {}, {"1", "1"}, {" 1"}, {"01"}, {"0"}, {"-1"}, {"9223372036854775808"}, tooMany} {
		if err := ValidateMetadataRead(MetadataReadRequest{PostIDs: ids}); !errors.Is(err, ErrInvalidMetadata) {
			t.Fatalf("ids %v: %v", ids, err)
		}
	}
}

func TestPostgresMetadataReadOrderingVisibilityAndNoMutation(t *testing.T) {
	ctx := context.Background()
	b := newMetadataTestBackend(t, ctx, metadataTestDatabaseURL(t))
	first := insertTaxPost(t, ctx, b, []string{"fixture"}, "", "one", "published", false)
	second := insertTaxPost(t, ctx, b, []string{}, "two", "", "published", false)
	hidden := insertTaxPost(t, ctx, b, []string{}, "private", "", "hidden", false)
	trashed := insertTaxPost(t, ctx, b, []string{}, "trash", "", "published", true)
	if _, err := b.pool.Exec(ctx, `UPDATE posts SET metadata_version=7 WHERE id=$1::bigint`, second); err != nil {
		t.Fatal(err)
	}
	got, err := b.ReadMetadata(ctx, MetadataReadRequest{PostIDs: []string{second, first}})
	want := []MetadataSnapshot{{ID: second, Version: 7, Source: "two"}, {ID: first, Version: 0, Artist: "one"}}
	if err != nil || !reflect.DeepEqual(got.Posts, want) {
		t.Fatalf("read %v: %v", got, err)
	}
	for _, inaccessible := range []string{hidden, trashed, "9223372036854775807"} {
		got, err := b.ReadMetadata(ctx, MetadataReadRequest{PostIDs: []string{first, inaccessible}})
		if !errors.Is(err, ErrNotFound) || len(got.Posts) != 0 {
			t.Fatalf("inaccessible %s returned %v, %v", inaccessible, got, err)
		}
	}
	var revisions int
	if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM post_metadata_revisions`).Scan(&revisions); err != nil || revisions != 0 {
		t.Fatalf("read changed history: count=%d, err=%v", revisions, err)
	}
}
