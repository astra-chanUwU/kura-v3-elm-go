package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMaintenanceConfig(t *testing.T) {
	for _, input := range []string{"", "30m", "0", "tomorrow"} {
		if _, err := ParseMaintenanceConfig(input, ""); input == "" {
			if err != nil {
				t.Fatalf("disabled config: %v", err)
			}
		} else if err == nil {
			t.Errorf("interval %q accepted", input)
		}
	}
	cfg, err := ParseMaintenanceConfig("1h", "")
	if err != nil || !cfg.Enabled || cfg.Interval != time.Hour || cfg.Keep != 7 {
		t.Fatalf("default config=%+v err=%v", cfg, err)
	}
	for _, input := range []string{"0", "-1", "wat"} {
		if _, err := ParseMaintenanceConfig("1h", input); err == nil {
			t.Errorf("keep %q accepted", input)
		}
	}
	cfg, err = ParseMaintenanceConfig("90m", "3")
	if err != nil || cfg.Keep != 3 || cfg.Interval != 90*time.Minute {
		t.Fatalf("configured values=%+v err=%v", cfg, err)
	}
	// KURA_BACKUP_KEEP alone does not turn on deletion.
	cfg, err = ParseMaintenanceConfig("", "1")
	if err != nil || cfg.Enabled {
		t.Fatalf("retention enabled without schedule: %+v err=%v", cfg, err)
	}
}

func TestLatestBackupTimeUsesJobsAndPublishedNames(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "backup-20261015T110000Z-00000001"), 0700); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	if err := s.writeJob(root, Job{ID: "backup-20261015T120000Z-00000002", Status: statusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "backup-unknown-time"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := latestBackupTime(context.Background(), root)
	want := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if err != nil || !got.Equal(want) {
		t.Fatalf("latest=%v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := latestBackupTime(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan err=%v", err)
	}
}

func TestRunMaintenanceSkipsRecentPersistedJob(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id := "backup-20261015T110000Z-00000001"
	if err := s.writeJob(root, Job{ID: id, Status: statusRunning}); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC) }
	s.maintenance = MaintenanceConfig{Enabled: true, Interval: time.Hour, Keep: 7}
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	runs := 0
	s.run = func(context.Context, []string, string, string) error {
		runs++
		return nil
	}
	s.lock = func(context.Context, []string) (func(), error) {
		// Model another process finishing a recent backup after this scheduler
		// observed the old job, but before it acquires the shared lock.
		if err := s.writeJob(root, Job{ID: "backup-20261015T120000Z-00000002", Status: statusRunning}); err != nil {
			return nil, err
		}
		return func() {}, nil
	}
	job, err := s.RunMaintenance(context.Background())
	if !errors.Is(err, ErrNotDue) || job.ID != "" || runs != 0 {
		t.Fatalf("job=%+v runs=%d err=%v", job, runs, err)
	}
}

func TestPruneKeepsNewestVerifiedAndPreservesUnknownInvalidAndSymlinked(t *testing.T) {
	root := t.TempDir()
	replicaRoot := t.TempDir()
	t.Setenv("KURA_BACKUP_REPLICA_ROOT", replicaRoot)
	s := New(root)
	ids := []string{
		"backup-20261012T120000Z-00000001",
		"backup-20261013T120000Z-00000002",
		"backup-20261014T120000Z-00000003",
	}
	for i, id := range ids {
		fixture(t, root, id)
		status := statusSucceeded
		if i == len(ids)-1 {
			status = statusRunning
		}
		if err := s.writeJob(root, Job{ID: id, Status: status}); err != nil {
			t.Fatal(err)
		}
		fixture(t, replicaRoot, id)
	}
	// An unrecorded but valid directory, a failed job, and an invalid archive
	// are all outside retention's authority and must survive pruning.
	fixture(t, root, "backup-20261015T120000Z-00000004")
	failed := "backup-20261016T120000Z-00000005"
	fixture(t, root, failed)
	if err := s.writeJob(root, Job{ID: failed, Status: statusFailed}); err != nil {
		t.Fatal(err)
	}
	invalid := "backup-20261017T120000Z-00000006"
	if err := os.Mkdir(filepath.Join(root, invalid), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	symlinkID := "backup-20261018T120000Z-00000007"
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(root, symlinkID)); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if err := s.writeJob(root, Job{ID: symlinkID, Status: statusSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := s.pruneLocked(context.Background(), root, 1, ids[2]); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ids[0], ids[1]} {
		if _, err := os.Lstat(filepath.Join(root, id)); !os.IsNotExist(err) {
			t.Errorf("old verified backup %s remains: %v", id, err)
		}
	}
	for _, id := range []string{ids[2], "backup-20261015T120000Z-00000004", failed, invalid, symlinkID} {
		if _, err := os.Lstat(filepath.Join(root, id)); err != nil {
			t.Errorf("protected backup %s was removed: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "target")); err != nil {
		t.Fatalf("symlink target changed: %v", err)
	}
}

func TestRetireVerifiedBackupRestoresPathSwapWithoutDeletingReplacement(t *testing.T) {
	root := t.TempDir()
	id := "backup-20261012T120000Z-00000001"
	source := filepath.Join(root, id)
	original := filepath.Join(root, "original-preserved")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "marker"), []byte("verified candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	err = retireVerifiedBackup(context.Background(), root, id, expected, func(context.Context, string) error {
		verifyCalls++
		return nil
	}, func() error {
		// Simulate an uncooperative writer replacing the candidate after the
		// inode check but before the atomic move into quarantine.
		if err := os.Rename(source, original); err != nil {
			return err
		}
		if err := os.Mkdir(source, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(source, "marker"), []byte("unknown replacement"), 0600)
	})
	if err == nil {
		t.Fatal("replacement path was retired without detecting the inode swap")
	}
	if verifyCalls != 0 {
		t.Fatalf("replacement was content-verified as the candidate: %d calls", verifyCalls)
	}
	for path, want := range map[string]string{source: "unknown replacement", original: "verified candidate"} {
		data, readErr := os.ReadFile(filepath.Join(path, "marker"))
		if readErr != nil || string(data) != want {
			t.Errorf("%s marker=%q err=%v", path, data, readErr)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".kura-retired-") {
			t.Fatalf("unexpected quarantine left after successful restore: %s", entry.Name())
		}
	}
}
