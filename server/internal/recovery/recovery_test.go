package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(filepath.Join(dir, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		p string
		b []byte
	}{{"database.dump", []byte("dump")}, {"media/pic.bin", []byte("image bytes")}}
	entries := make([]fileEntry, 0, len(files))
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f.p)), f.b, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(f.b)
		entries = append(entries, fileEntry{f.p, int64(len(f.b)), hex.EncodeToString(sum[:])})
	}
	m := manifest{Version: 1, CreatedAt: "2026-10-15T12:00:00Z", Files: entries}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestListAndVerifyManifestHashes(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "nightly-1")
	s := New(root)
	l, err := s.List(context.Background())
	if err != nil || !l.Configured || len(l.Backups) != 1 || l.Backups[0].Status != "available" || l.Backups[0].MediaCount != 1 || l.Backups[0].Bytes != 15 {
		t.Fatalf("listing=%+v err=%v", l, err)
	}
	r, err := s.Verify(context.Background(), "nightly-1")
	if err != nil || r.ID != "nightly-1" || r.MediaCount != 1 {
		t.Fatalf("receipt=%+v err=%v", r, err)
	}
	if err := os.WriteFile(filepath.Join(root, "nightly-1", "media", "pic.bin"), []byte("altered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(context.Background(), "nightly-1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected hash failure, got %v", err)
	}
	fixture(t, root, "dump-check")
	if err := os.WriteFile(filepath.Join(root, "dump-check", "database.dump"), []byte("changed dump"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(context.Background(), "dump-check"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("dump hash mismatch accepted: %v", err)
	}
}

func TestRejectTraversalSymlinksAndMalformedManifest(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "valid")
	s := New(root)
	for _, id := range []string{"../valid", "/valid", "..", "a/b"} {
		if _, err := s.Verify(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q: %v", id, err)
		}
	}
	dir := filepath.Join(root, "bad")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":99}`), 0600)
	l, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range l.Backups {
		if b.ID == "bad" && b.Status == "invalid" {
			found = true
		}
	}
	if !found {
		t.Fatalf("malformed archive not marked invalid: %+v", l)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "valid"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	l, err = s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	linkedInvalid := false
	for _, b := range l.Backups {
		if b.ID == "linked" && b.Status == "invalid" {
			linkedInvalid = true
		}
	}
	if !linkedInvalid {
		t.Fatalf("backup symlink was not rejected: %+v", l)
	}
	if err := os.Symlink(outside, filepath.Join(root, "valid", "media", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(context.Background(), "valid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink accepted: %v", err)
	}
}

func TestUnconfiguredCanceledAndBusy(t *testing.T) {
	if _, err := New("").Verify(context.Background(), "x"); !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("unconfigured: %v", err)
	}
	root := t.TempDir()
	fixture(t, root, "backup")
	s := New(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Verify(ctx, "backup"); !errors.Is(err, ErrCanceled) {
		t.Fatalf("cancel: %v", err)
	}
	timedOut, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if _, err := s.Verify(timedOut, "backup"); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	s.verifying.Store(true)
	defer s.verifying.Store(false)
	if _, err := s.Verify(context.Background(), "backup"); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
}
