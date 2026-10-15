package posts

import (
	"context"
	"testing"
)

func TestPrivateLibraryRejectsExternalMediaReferences(t *testing.T) {
	ctx := context.Background()
	backend, _ := newSimilarityBackend(t, ctx)
	post := uploadSimilarityFixture(t, backend, ctx, 0, "private.jpg")
	for _, url := range []string{"https://external.invalid/image.jpg", "HTTPS://external.invalid/image.jpg", "//external.invalid/image.jpg", "data:image/png;base64,fixture"} {
		if _, err := backend.pool.Exec(ctx, `UPDATE posts SET original_url = $2 WHERE id = $1::bigint`, post.ID, url); err != nil {
			t.Fatal(err)
		}
		external, err := backend.HasExternalMediaURLs(ctx)
		if err != nil || !external {
			t.Fatalf("external media reference was accepted: external=%v error=%v", external, err)
		}
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE posts SET original_url = '/media/uploads/fixture.jpg', preview_url = '/static/icons/shelf.svg' WHERE id = $1::bigint`, post.ID); err != nil {
		t.Fatal(err)
	}
	if external, err := backend.HasExternalMediaURLs(ctx); err != nil || external {
		t.Fatalf("local gated media was rejected: external=%v error=%v", external, err)
	}
}
