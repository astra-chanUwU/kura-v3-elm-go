package posts

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

func TestExportAndTagQueryValidation(t *testing.T) {
	for _, r := range []ExportRequest{{}, {PostIDs: []string{"01"}}, {PostIDs: []string{"1", "1"}}, {PostIDs: []string{"1"}, CollectionID: "2"}, {CollectionID: "2"}, {PostIDs: []string{"9223372036854775808"}}} {
		if err := ValidateExportRequest(r); err == nil {
			t.Fatalf("accepted %#v", r)
		}
	}
	if err := ValidateExportRequest(ExportRequest{PostIDs: []string{"7", "8"}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{strings.Repeat("a", 129), "bad\x00tag"} {
		if _, err := ValidateTagSuggestionQuery(p, 12); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	for _, n := range []int{0, 51} {
		if _, err := ValidateTagSuggestionQuery("ca", n); err == nil {
			t.Fatalf("accepted limit%d", n)
		}
	}
}

func TestLibraryFeaturesPostgres(t *testing.T) {
	ctx := context.Background()
	backend := newCollectionPaginationBackend(t, ctx, collectionPaginationTestURL(t))
	ids := seedPublishedPosts(t, ctx, backend, 4)
	root := t.TempDir()
	backend.store = media.NewLocalStore(root)
	contents := [][]byte{[]byte("first original"), []byte("second original")}
	for i, data := range contents {
		name := ids[i] + ".jpg"
		if err := os.MkdirAll(filepath.Join(root, "uploads"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "uploads", name), data, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if _, err := backend.pool.Exec(ctx, `UPDATE posts SET original_url=$2, hash=$3, file_size=$4,tags='{cat,catfish,c%literal}',source='kept-source',artist='kept-artist' WHERE id=$1::bigint`, ids[i], "/media/uploads/"+name, "sha256:"+hex.EncodeToString(digest[:]), len(data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET tags='{cat,secret}',moderation_state='hidden' WHERE id=$1::bigint`, ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET tags='{cat,trashed}',deleted_at=now() WHERE id=$1::bigint`, ids[3]); err != nil {
		t.Fatal(err)
	}
	suggestions, err := backend.SuggestTags(ctx, "ca", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 2 || suggestions[0].Count != 2 || suggestions[1].Count != 2 {
		t.Fatalf("suggestions=%#v", suggestions)
	}
	literal, err := backend.SuggestTags(ctx, "c%", 12)
	if err != nil || len(literal) != 1 || literal[0].Tag != "c%literal" {
		t.Fatalf("literal prefix=%#v err=%v", literal, err)
	}
	all, err := backend.SuggestTags(ctx, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.Tag == "secret" || s.Tag == "trashed" {
			t.Fatal("private tag leaked")
		}
	}
	collection, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	collection, err = backend.AddCollectionPosts(ctx, collection.ID, AddCollectionPostsRequest{PostIDs: ids[:2]})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := backend.RenameCollection(ctx, collection.ID, CollectionManageRequest{Name: "Renamed", ExpectedVersion: collection.Version})
	if err != nil || renamed.Version != collection.Version+1 || !renamed.Changed {
		t.Fatalf("rename=%#v err=%v", renamed, err)
	}
	noop, err := backend.RenameCollection(ctx, collection.ID, CollectionManageRequest{Name: "Renamed", ExpectedVersion: renamed.Version})
	if err != nil || noop.Changed || noop.Version != renamed.Version {
		t.Fatalf("noop=%#v err=%v", noop, err)
	}
	if _, err = backend.DeleteCollection(ctx, collection.ID, collection.Version); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("stale delete=%v", err)
	}
	archive, err := backend.ExportLibrary(ctx, ExportRequest{PostIDs: []string{ids[1], ids[0]}, Metadata: true})
	if err != nil {
		t.Fatal(err)
	}
	name := archive.File.Name()
	zr, err := zip.NewReader(archive.File, archive.Size)
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 3 {
		t.Fatalf("zip files=%d", len(zr.File))
	}
	for i := 0; i < 2; i++ {
		r, err := zr.File[i].Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(data) != string(contents[1-i]) {
			t.Fatalf("bytes=%q err=%v", data, err)
		}
	}
	r, err := zr.File[2].Open()
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version int          `json:"version"`
		Posts   []exportPost `json:"posts"`
	}
	err = json.NewDecoder(r).Decode(&manifest)
	r.Close()
	if err != nil || manifest.Version != 1 || manifest.Posts[0].Source != "kept-source" || manifest.Posts[0].ID != ids[1] {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("staged archive leaked")
	}
	for _, id := range ids[2:] {
		if _, err = backend.ExportLibrary(ctx, ExportRequest{PostIDs: []string{id}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("private export=%v", err)
		}
	}
	version := renamed.Version
	archive, err = backend.ExportLibrary(ctx, ExportRequest{CollectionID: collection.ID, ExpectedVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	archive.Close()
	version--
	if _, err = backend.ExportLibrary(ctx, ExportRequest{CollectionID: collection.ID, ExpectedVersion: &version}); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("stale export=%v", err)
	}
	if err = os.WriteFile(filepath.Join(root, "uploads", ids[0]+".jpg"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = backend.ExportLibrary(ctx, ExportRequest{PostIDs: []string{ids[0]}}); err == nil {
		t.Fatal("tampered original exported")
	}
	deleted, err := backend.DeleteCollection(ctx, collection.ID, renamed.Version)
	if err != nil || !deleted.Changed {
		t.Fatalf("delete=%#v err=%v", deleted, err)
	}
	var count int
	if err = backend.pool.QueryRow(ctx, `SELECT COUNT(*) FROM posts`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("posts count=%d err=%v", count, err)
	}
	// A collection with no currently public members remains listed.
	empty, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: "Hidden only"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = backend.pool.Exec(ctx, `INSERT INTO collection_posts(collection_id,post_id,position) VALUES($1::bigint,$2::bigint,0)`, empty.ID, ids[2]); err != nil {
		t.Fatal(err)
	}
	listed, err := backend.ListCollections(ctx)
	if err != nil || len(listed) != 1 || len(listed[0].PostIDs) != 0 {
		t.Fatalf("legacy hidden collection=%#v err=%v", listed, err)
	}
}
