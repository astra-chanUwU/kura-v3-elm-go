package posts

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

func TestResolveStoredMediaKeyUsesUploadDerivativeAndImportWriters(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	backend := newUploadTestBackend(t, ctx, uploadTestDatabaseURL(t), root)

	uploaded, err := backend.CreateUpload(ctx, UploadRequest{
		File:     bytes.NewReader(testPNG(t, 32, 24)),
		Filename: "mapped.png",
		Tags:     []string{"mapped"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResolvedKey(t, ctx, backend, uploaded.OriginalURL, uploaded.MediaType)
	if !strings.HasPrefix(uploaded.OriginalURL, "/media/uploads/") {
		t.Fatalf("upload url %q", uploaded.OriginalURL)
	}

	if !runDerivativeOnce(t, backend) {
		t.Fatal("derivative worker claimed no job")
	}
	ready, err := backend.GetPostDetail(ctx, uploaded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.PreviewURL == ready.OriginalURL || !strings.HasPrefix(ready.PreviewURL, "/media/derivatives/") {
		t.Fatalf("derivative preview %q", ready.PreviewURL)
	}
	thumbKey, thumbType, err := backend.ResolveStoredMediaKey(ctx, ready.PreviewURL)
	if err != nil {
		t.Fatal(err)
	}
	if thumbType != "image/jpeg" || !strings.HasPrefix(thumbKey, "derivatives/thumb-320/") {
		t.Fatalf("thumbnail key %q type %q", thumbKey, thumbType)
	}
	originalKey, _, err := backend.ResolveStoredMediaKey(ctx, ready.OriginalURL)
	if err != nil {
		t.Fatal(err)
	}
	if originalKey == thumbKey {
		t.Fatal("thumbnail URL resolved to the original object")
	}

	importedPNG := testPNG(t, 8, 8)
	imported, err := backend.ImportBrowser(ctx, browserImportRequest(importedPNG, "imported.png"))
	if err != nil {
		t.Fatal(err)
	}
	assertResolvedKey(t, ctx, backend, imported.Post.OriginalURL, imported.Post.MediaType)

	if _, err := backend.ModeratePosts(ctx, "test", ModerationRequest{
		Posts:  []ModerationTarget{{ID: uploaded.ID}},
		Action: ModerationActionHide,
	}); err != nil {
		t.Fatal(err)
	}
	assertMediaDenied(t, ctx, backend, ready.OriginalURL)
	assertMediaDenied(t, ctx, backend, ready.PreviewURL)
	if _, err := backend.ModeratePosts(ctx, "test", ModerationRequest{
		Posts:  []ModerationTarget{{ID: uploaded.ID}},
		Action: ModerationActionReject,
	}); err != nil {
		t.Fatal(err)
	}
	assertMediaDenied(t, ctx, backend, ready.OriginalURL)
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE id = $1::bigint`, imported.Post.ID); err != nil {
		t.Fatal(err)
	}
	assertMediaDenied(t, ctx, backend, imported.Post.OriginalURL)

	for _, reference := range []string{
		"https://cdn.example.com/uploads/abc.jpg",
		"//cdn.example.com/uploads/abc.jpg",
		"/media/../secret",
		"/media/uploads/../../secret",
	} {
		if _, _, err := backend.ResolveStoredMediaKey(ctx, reference); err == nil {
			t.Fatalf("resolved %q", reference)
		}
	}
}

func TestPrivateSameOriginS3IsNotExternal(t *testing.T) {
	sameOrigin, err := media.NewS3Store(media.S3Config{Endpoint: "http://127.0.0.1:9", Bucket: "fixture", URLPrefix: "/media"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := media.NewS3Store(media.S3Config{Endpoint: "http://127.0.0.1:9", Bucket: "fixture", URLPrefix: "https://cdn.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	backend := &PostgresBackend{store: sameOrigin}
	if backend.UsesExternalMediaProvider() {
		t.Fatal("same-origin S3 was treated as an external provider")
	}
	backend.store = public
	if !backend.UsesExternalMediaProvider() {
		t.Fatal("public S3 URLs must stay external")
	}
	backend.store = media.NewLocalStore(t.TempDir())
	if backend.UsesExternalMediaProvider() {
		t.Fatal("local store was treated as external")
	}
}

func assertMediaDenied(t *testing.T, ctx context.Context, backend *PostgresBackend, reference string) {
	t.Helper()
	visibility, err := backend.LookupMediaVisibility(ctx, reference)
	if err != nil {
		t.Fatal(err)
	}
	if !visibility.Referenced || visibility.Visible {
		t.Fatalf("visibility for %q = %+v", reference, visibility)
	}
}

func assertResolvedKey(t *testing.T, ctx context.Context, backend *PostgresBackend, reference, mediaType string) {
	t.Helper()
	key, gotType, err := backend.ResolveStoredMediaKey(ctx, reference)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := media.KeyFromStoredMediaURL(reference)
	if err != nil || key != mapped || gotType != mediaType {
		t.Fatalf("reference %q key %q mapped %q type %q/%q err %v", reference, key, mapped, gotType, mediaType, err)
	}
	body, err := backend.store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = body.Close()
}
