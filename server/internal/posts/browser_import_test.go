package posts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBrowserImportHashValidationAndBounds(t *testing.T) {
	for _, value := range []string{"", "sha256:short", "sha256:" + strings.Repeat("A", 64), "md5:" + strings.Repeat("a", 64)} {
		if validCanonicalSHA256(value) {
			t.Fatalf("accepted malformed hash %q", value)
		}
	}
	if !validCanonicalSHA256("sha256:" + strings.Repeat("0", 64)) {
		t.Fatal("rejected canonical hash")
	}
	for _, hashes := range [][]string{nil, make([]string, 101), []string{"sha256:bad"}} {
		if err := validateBrowserDuplicateHashes(hashes); !errors.Is(err, ErrInvalidUpload) {
			t.Fatalf("expected malformed/oversized list rejection, got %v", err)
		}
	}
}

func browserImportBackend(t *testing.T, root string) *PostgresBackend {
	t.Helper()
	ctx := context.Background()
	backend := newUploadTestBackend(t, ctx, uploadTestDatabaseURL(t), root)
	return backend
}

func browserImportRequest(data []byte, filename string) BrowserImportRequest {
	return BrowserImportRequest{UploadRequest: UploadRequest{File: bytes.NewReader(data), Filename: filename, Tags: []string{"browser-import"}}, ExpectedHash: browserExpectedHash(data)}
}

func browserExpectedHash(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func TestBrowserDuplicateLookupOnlyReturnsPublicPublishedPosts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := browserImportBackend(t, root)
	data := testPNG(t, 4, 3)
	first, err := backend.CreateUpload(ctx, UploadRequest{File: bytes.NewReader(data), Filename: "first.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET moderation_state='pending' WHERE id=$1::bigint`, first.ID); err != nil {
		t.Fatal(err)
	}
	matches, err := backend.FindBrowserImportDuplicates(ctx, []string{first.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("private pending post leaked in duplicate lookup: %#v", matches)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET moderation_state='published', deleted_at=now() WHERE id=$1::bigint`, first.ID); err != nil {
		t.Fatal(err)
	}
	matches, err = backend.FindBrowserImportDuplicates(ctx, []string{first.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("deleted post leaked in duplicate lookup: %#v", matches)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at=NULL WHERE id=$1::bigint`, first.ID); err != nil {
		t.Fatal(err)
	}
	matches, err = backend.FindBrowserImportDuplicates(ctx, []string{first.Hash})
	if err != nil || len(matches) != 1 || matches[0].PostID != first.ID {
		t.Fatalf("public duplicate result=%#v err=%v", matches, err)
	}
}

func TestBrowserImportConcurrentSkipCreatesOnePost(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := browserImportBackend(t, root)
	data := testPNG(t, 8, 5)
	start := make(chan struct{})
	results := make([]BrowserImportResult, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for i := range results {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			results[i], errs[i] = backend.ImportBrowser(ctx, browserImportRequest(data, fmt.Sprintf("%d.png", i)))
		}(i)
	}
	close(start)
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].Post.ID != results[1].Post.ID || results[0].Duplicate == results[1].Duplicate {
		t.Fatalf("unexpected concurrent results: %#v", results)
	}
	var count int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM posts WHERE hash=$1 AND deleted_at IS NULL`, results[0].Post.Hash).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same content produced %d posts", count)
	}
}

func TestBrowserImportRejectsInvalidCollectionBeforeCreatingPost(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := browserImportBackend(t, root)
	_, err := backend.ImportBrowser(ctx, BrowserImportRequest{UploadRequest: UploadRequest{File: bytes.NewReader(testPNG(t, 2, 2)), Filename: "bad.png"}, CollectionID: "987654321"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing collection, got %v", err)
	}
	var count int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM posts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid collection left %d durable posts", count)
	}
	files, err := os.ReadDir(filepath.Join(root, "uploads"))
	if err == nil && len(files) != 0 {
		t.Fatalf("invalid collection stored media first: %v", files)
	}
}

func TestBrowserImportRejectsChangedExpectedHashBeforeDurableWrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := browserImportBackend(t, root)
	data := testPNG(t, 2, 4)
	request := BrowserImportRequest{UploadRequest: UploadRequest{File: bytes.NewReader(data), Filename: "changed.png"}, ExpectedHash: "sha256:" + strings.Repeat("0", 64)}
	if _, err := backend.ImportBrowser(ctx, request); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("expected stale expected_hash rejection, got %v", err)
	}
	var count int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM posts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("hash mismatch left %d durable posts", count)
	}
	entries, err := os.ReadDir(filepath.Join(root, "uploads"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("hash mismatch stored media: %v", entries)
	}
}

func TestBrowserImportDuplicatePoliciesAndIdempotentAttachment(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := browserImportBackend(t, root)
	collection, err := backend.CreateCollection(ctx, CreateCollectionRequest{Name: fmt.Sprintf("Browser import test %d", time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	data := testPNG(t, 3, 7)
	first, err := backend.ImportBrowser(ctx, BrowserImportRequest{UploadRequest: UploadRequest{File: bytes.NewReader(data), Filename: "first.png"}, ExpectedHash: browserExpectedHash(data), CollectionID: collection.ID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate || !first.Attached || first.CollectionID == nil || *first.CollectionID != collection.ID {
		t.Fatalf("first import result=%#v", first)
	}
	_, err = backend.ImportBrowser(ctx, BrowserImportRequest{UploadRequest: UploadRequest{File: bytes.NewReader(data), Filename: "again.png"}, ExpectedHash: browserExpectedHash(data), DuplicatePolicy: "error"})
	if !errors.Is(err, ErrDuplicateImport) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	second, err := backend.ImportBrowser(ctx, BrowserImportRequest{UploadRequest: UploadRequest{File: bytes.NewReader(data), Filename: "again.png"}, ExpectedHash: browserExpectedHash(data), CollectionID: collection.ID, DuplicatePolicy: "skip"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || !second.Attached || second.Post.ID != first.Post.ID {
		t.Fatalf("duplicate result=%#v", second)
	}
	var members, versionBumps int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM collection_posts WHERE collection_id=$1::bigint AND post_id=$2::bigint`, collection.ID, first.Post.ID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT version FROM collections WHERE id=$1::bigint`, collection.ID).Scan(&versionBumps); err != nil {
		t.Fatal(err)
	}
	if members != 1 || versionBumps != 1 {
		t.Fatalf("members=%d collection version=%d; duplicate attach should be idempotent", members, versionBumps)
	}
}
