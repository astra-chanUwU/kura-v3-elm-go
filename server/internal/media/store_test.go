package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorePutIsContentAddressedAndIdempotent(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	first, err := store.Put(context.Background(), "uploads/abc.jpg", "image/jpeg", strings.NewReader("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.URL != "/media/uploads/abc.jpg" || first.Size != 5 {
		t.Fatalf("unexpected first object: %#v", first)
	}
	second, err := store.Put(context.Background(), "uploads/abc.jpg", "image/jpeg", strings.NewReader("different"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.Size != 5 {
		t.Fatalf("existing object was overwritten: %#v", second)
	}
	data, err := os.ReadFile(filepath.Join(store.Root, "uploads", "abc.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "bytes" {
		t.Fatalf("stored bytes=%q", data)
	}
}

func TestLocalStoreRejectsTraversalAndDeletes(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	if _, err := store.Put(context.Background(), "../escape", "application/octet-stream", strings.NewReader("x")); err == nil {
		t.Fatal("expected traversal key to fail")
	}
	object, err := store.Put(context.Background(), "uploads/remove.jpg", "image/jpeg", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), object.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "uploads", "remove.jpg")); !os.IsNotExist(err) {
		t.Fatalf("object still exists: %v", err)
	}
}
