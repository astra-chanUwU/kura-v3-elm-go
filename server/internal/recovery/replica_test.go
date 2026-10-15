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
)

func TestReplicateCopiesAndVerifiesBeforePublication(t *testing.T) {
	primary, replica := t.TempDir(), t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", filepath.Join(t.TempDir(), "media"))
	backupID := "nightly-1"
	replicaFixture(t, primary, backupID, []byte("database snapshot"), []byte("image bytes"))

	service := New(primary)
	receipt, err := service.Replicate(context.Background(), backupID)
	if err != nil {
		t.Fatalf("replicate: %v", err)
	}
	if receipt.ID != backupID || receipt.MediaCount != 1 || receipt.Bytes != int64(len("database snapshot")+len("image bytes")) || receipt.ReplicatedAt == "" {
		t.Fatalf("receipt=%+v", receipt)
	}
	if _, err := New(replica).Verify(context.Background(), backupID); err != nil {
		t.Fatalf("replica does not verify: %v", err)
	}
	if _, err := service.Verify(context.Background(), backupID); err != nil {
		t.Fatalf("primary was removed or changed: %v", err)
	}
	entries, err := os.ReadDir(replica)
	if err != nil || len(entries) != 1 || entries[0].Name() != backupID {
		t.Fatalf("replica root entries=%v err=%v", entries, err)
	}
}

func TestReplicateCopiesPayloadsLargerThanManifestLimitInBoundedChunks(t *testing.T) {
	primary, replica := t.TempDir(), t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", "")
	largeDump := make([]byte, 20<<20)
	largeMedia := make([]byte, 20<<20)
	for i := range largeDump {
		largeDump[i] = byte(i * 31)
		largeMedia[i] = byte(i*17 + 11)
	}
	replicaFixture(t, primary, "large", largeDump, largeMedia)
	if _, err := New(primary).Replicate(context.Background(), "large"); err != nil {
		t.Fatalf("replicate large fixture: %v", err)
	}
	if _, err := New(replica).Verify(context.Background(), "large"); err != nil {
		t.Fatalf("replicated large fixture did not verify: %v", err)
	}
}

func TestReplicateRequiresConfiguredSeparateRoot(t *testing.T) {
	primary := t.TempDir()
	replica := t.TempDir()
	replicaFixture(t, primary, "backup", []byte("dump"), []byte("media"))
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", "")
	if _, err := New(primary).Replicate(context.Background(), "backup"); !errors.Is(err, ErrReplicaUnconfigured) {
		t.Fatalf("missing replica root err=%v", err)
	}
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", primary)
	if _, err := New(primary).Replicate(context.Background(), "backup"); !errors.Is(err, ErrReplicaRoot) {
		t.Fatalf("same root err=%v", err)
	}
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", filepath.Join(replica, "uploads"))
	if _, err := New(primary).Replicate(context.Background(), "backup"); !errors.Is(err, ErrReplicaRoot) {
		t.Fatalf("replica under media root err=%v", err)
	}
}

func TestReplicateNeverOverwritesExistingDestination(t *testing.T) {
	primary, replica := t.TempDir(), t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", "")
	replicaFixture(t, primary, "backup", []byte("primary"), []byte("media"))
	destination := filepath.Join(replica, "backup")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(destination, "keep")
	if err := os.WriteFile(marker, []byte("existing destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(primary).Replicate(context.Background(), "backup"); !errors.Is(err, ErrReplicaExists) {
		t.Fatalf("existing destination err=%v", err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "existing destination" {
		t.Fatalf("existing destination changed: %q err=%v", b, err)
	}
}

func TestReplicaRenameRefusesRacingEmptyDestination(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, ".stage")
	destination := filepath.Join(root, "backup")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "staged"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := replicaRenameNoReplace(stage, destination); err == nil {
		t.Fatal("atomic no-replace rename replaced an existing empty directory")
	}
	if _, err := os.Stat(filepath.Join(stage, "staged")); err != nil {
		t.Fatalf("failed publish moved the staging directory: %v", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatalf("existing empty destination changed: entries=%v err=%v", entries, err)
	}
}

func TestReplicateRejectsInvalidSourceWithoutPublishing(t *testing.T) {
	primary, replica := t.TempDir(), t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", "")
	replicaFixture(t, primary, "corrupt", []byte("dump"), []byte("media"))
	if err := os.WriteFile(filepath.Join(primary, "corrupt", "database.dump"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(primary).Replicate(context.Background(), "corrupt"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt source err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(replica, "corrupt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt source was published: %v", err)
	}
	entries, err := os.ReadDir(replica)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed replication left staging content: %v err=%v", entries, err)
	}
}

func TestReplicateCancellationPublishesNothing(t *testing.T) {
	primary, replica := t.TempDir(), t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", "")
	replicaFixture(t, primary, "backup", []byte("dump"), []byte("media"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(primary).Replicate(ctx, "backup"); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled replication err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(replica, "backup")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled replication published destination: %v", err)
	}
}

func TestReplicateRejectsSymlinkedSourceFile(t *testing.T) {
	primary, replica := t.TempDir(), t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replica)
	t.Setenv("MEDIA_ROOT", "")
	replicaFixture(t, primary, "linked", []byte("dump"), []byte("media"))
	path := filepath.Join(primary, "linked", "media", "asset.bin")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := New(primary).Replicate(context.Background(), "linked"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink source err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(replica, "linked")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink source was published: %v", err)
	}
}

func replicaFixture(t *testing.T, root, id string, dump, media []byte) {
	t.Helper()
	dir := filepath.Join(root, id)
	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		path string
		data []byte
	}{{"database.dump", dump}, {"media/asset.bin", media}}
	entries := make([]fileEntry, 0, len(files))
	for _, file := range files {
		name := filepath.Join(dir, filepath.FromSlash(file.path))
		if err := os.WriteFile(name, file.data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(file.data)
		entries = append(entries, fileEntry{Path: file.path, Size: int64(len(file.data)), SHA256: hex.EncodeToString(sum[:])})
	}
	body, err := json.Marshal(manifest{Version: 1, CreatedAt: "2026-10-15T12:00:00Z", Files: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}
