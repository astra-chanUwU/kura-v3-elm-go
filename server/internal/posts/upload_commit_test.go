package posts

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
)

func TestAmbiguousCommitRetainsNewUploadObject(t *testing.T) {
	ctx := context.Background()
	store := media.NewLocalStore(t.TempDir())
	created, err := store.Put(ctx, "uploads/ambiguous.png", "image/png", bytes.NewReader([]byte("image bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created {
		t.Fatal("test media object was not newly created")
	}
	backend := &PostgresBackend{store: store}
	object := stagedMediaObject{key: created.Key, url: created.URL, created: created.Created, store: store}
	keepObject := false
	defer func() {
		if !keepObject {
			backend.cleanupStagedObject(object)
		}
	}()
	commitErr := errors.New("connection lost while awaiting commit result")
	if err := commitRetainingObject(&keepObject, func() error { return commitErr }); !errors.Is(err, commitErr) {
		t.Fatalf("commit error=%v, want %v", err, commitErr)
	}
	if !keepObject {
		t.Fatal("media cleanup remained enabled after an ambiguous commit attempt")
	}
	reader, err := store.Open(ctx, created.Key)
	if err != nil {
		t.Fatalf("ambiguous commit removed the possibly durable original: %v", err)
	}
	_ = reader.Close()
}
