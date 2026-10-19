package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartRejectsRemoteStorageAndKeepsBrowsingLockFree(t *testing.T) {
	t.Setenv("S3_BUCKET", "example")
	service := New(t.TempDir())
	if _, err := service.Start(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestStartFailureCancelAndBusyNeverPublish(t *testing.T) {
	root := t.TempDir()
	media := t.TempDir()
	t.Setenv("MEDIA_ROOT", media)
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	t.Setenv("S3_BUCKET", "")
	service := New(root)
	var mu sync.Mutex
	held := false
	var calls int
	started := make(chan struct{})
	service.SetTestBackend(
		func(ctx context.Context, _ []string, _, _ string) error {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}
			return errors.New("database dump failed: pg_dump failed")
		},
		func(context.Context, []string) (func(), error) {
			mu.Lock()
			defer mu.Unlock()
			if held {
				return nil, ErrActive
			}
			held = true
			return func() {
				mu.Lock()
				held = false
				mu.Unlock()
			}, nil
		},
	)
	first, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := service.Start(context.Background()); !errors.Is(err, ErrActive) {
		t.Fatalf("second start err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, first.ID)); !os.IsNotExist(err) {
		t.Fatal("running backup was published")
	}
	if _, err := service.Cancel(first.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var job Job
	for time.Now().Before(deadline) {
		job, err = service.Job(first.ID)
		if err == nil && job.Status == statusCanceled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != statusCanceled {
		t.Fatalf("status=%s err=%v", job.Status, err)
	}
	if _, err := os.Stat(filepath.Join(root, first.ID)); !os.IsNotExist(err) {
		t.Fatal("canceled backup was published")
	}
	listing, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Backups) != 0 {
		t.Fatalf("unpublished names were listed: %+v", listing.Backups)
	}

	var failed Job
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		failed, err = service.Start(context.Background())
		if err == nil {
			break
		}
		if !errors.Is(err, ErrActive) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || failed.ID == "" {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err = service.Job(failed.ID)
		if err == nil && job.Status == statusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != statusFailed || !strings.Contains(job.Error, "dump failed") {
		t.Fatalf("job=%+v", job)
	}
	if strings.Contains(job.Error, "://") || strings.Contains(strings.ToLower(job.Error), "password") {
		t.Fatalf("job error leaked connection data: %s", job.Error)
	}
}

func TestShutdownCancelsActiveBackupAndWaitsForLockRelease(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	t.Setenv("S3_BUCKET", "")
	service := New(root)
	started := make(chan struct{})
	runnerStopped := make(chan struct{})
	lockReleased := make(chan struct{})
	service.SetTestBackend(
		func(ctx context.Context, _ []string, _, _ string) error {
			close(started)
			<-ctx.Done()
			close(runnerStopped)
			return ctx.Err()
		},
		func(context.Context, []string) (func(), error) {
			return func() { close(lockReleased) }, nil
		},
	)
	job, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-runnerStopped:
	default:
		t.Fatal("shutdown returned before the backup runner stopped")
	}
	select {
	case <-lockReleased:
	default:
		t.Fatal("shutdown returned before the advisory lock was released")
	}
	if _, err := service.Start(context.Background()); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("new backup accepted after shutdown: %v", err)
	}
	terminal, err := service.Job(job.ID)
	if err != nil || terminal.Status != statusCanceled {
		t.Fatalf("terminal job=%+v err=%v", terminal, err)
	}
}

func TestSuccessfulRunnerWithoutValidPublicationFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	service := New(root)
	service.SetTestBackend(
		func(context.Context, []string, string, string) error { return nil },
		func(context.Context, []string) (func(), error) { return func() {}, nil },
	)
	job, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err = service.Job(job.ID)
		if err == nil && job.Status != statusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != statusFailed || job.Error != "backup failed" {
		t.Fatalf("empty successful runner result was trusted: %+v", job)
	}
}

func TestSuccessfulPublicationIsListedAndVerified(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	service := New(root)
	service.SetTestBackend(
		func(_ context.Context, _ []string, _, output string) error {
			if err := os.MkdirAll(output, 0o700); err != nil {
				return err
			}
			data := []byte("dump-bytes")
			if err := os.WriteFile(filepath.Join(output, "database.dump"), data, 0o600); err != nil {
				return err
			}
			sum := sha256Hex(data)
			manifest := []byte(`{"version":1,"created_at":"2026-10-15T12:00:00Z","files":[{"path":"database.dump","size":10,"sha256":"` + sum + `"}]}` + "\n")
			return os.WriteFile(filepath.Join(output, "manifest.json"), manifest, 0o600)
		},
		func(context.Context, []string) (func(), error) { return func() {}, nil },
	)
	job, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := job.ID
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err = service.Job(id)
		if err == nil && job.Status == statusSucceeded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != statusSucceeded {
		body, _ := os.ReadFile(filepath.Join(root, ".kura-backup-jobs", id+".json"))
		t.Fatalf("job=%+v err=%v body=%s", job, err, body)
	}
	service.SetTestBackend(nil, func(context.Context, []string) (func(), error) { return func() {}, nil })
	if err := service.writeJob(root, Job{ID: job.ID, Status: statusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(); err != nil {
		t.Fatal(err)
	}
	reconciled, err := service.Job(job.ID)
	if err != nil || reconciled.Status != statusSucceeded {
		t.Fatalf("valid published snapshot was not reconciled: job=%+v err=%v", reconciled, err)
	}
	listing, err := service.List(context.Background())
	if err != nil || len(listing.Backups) != 1 || listing.Backups[0].ID != job.ID || listing.Backups[0].Status != "available" {
		t.Fatalf("listing=%+v err=%v", listing, err)
	}
	if _, err := service.Verify(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, ".kura-backup-partial")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	listing, err = service.List(context.Background())
	if err != nil || len(listing.Backups) != 1 {
		t.Fatalf("staging became visible: %+v %v", listing, err)
	}
}

func TestReconcileDoesNotPromoteAnInterruptedAttempt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	service := New(root)
	service.SetTestBackend(nil, func(context.Context, []string) (func(), error) { return func() {}, nil })
	if err := service.writeJob(root, Job{ID: "backup-interrupted", Status: statusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".kura-backup-partial"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(); err != nil {
		t.Fatal(err)
	}
	job, err := service.Job("backup-interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != statusFailed {
		t.Fatalf("status=%s", job.Status)
	}
	if _, err := os.Stat(filepath.Join(root, ".kura-backup-partial")); !os.IsNotExist(err) {
		t.Fatal("staging survived restart")
	}
	listing, err := service.List(context.Background())
	if err != nil || len(listing.Backups) != 0 {
		t.Fatalf("listing=%+v err=%v", listing, err)
	}
}

func TestReconcileDoesNotPromoteInvalidPublication(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	service := New(root)
	service.SetTestBackend(nil, func(context.Context, []string) (func(), error) { return func() {}, nil })
	job := Job{ID: "backup-invalid", Status: statusRunning}
	if err := service.writeJob(root, job); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, job.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, job.ID, "database.dump"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(); err != nil {
		t.Fatal(err)
	}
	job, err := service.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != statusFailed {
		t.Fatalf("invalid publication was promoted: %+v", job)
	}
}

func TestReconcileSkipsLiveJobOwnedByAnotherService(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATABASE_URL", "postgresql://postgres@127.0.0.1:1/kura_test?sslmode=disable")
	var mu sync.Mutex
	held := false
	lock := func(context.Context, []string) (func(), error) {
		mu.Lock()
		if held {
			mu.Unlock()
			return nil, ErrActive
		}
		held = true
		mu.Unlock()
		return func() {
			mu.Lock()
			held = false
			mu.Unlock()
		}, nil
	}
	started := make(chan struct{})
	serviceA := New(root)
	serviceA.SetTestBackend(func(ctx context.Context, _ []string, _, _ string) error {
		staging := filepath.Join(root, ".kura-backup-live")
		if err := os.Mkdir(staging, 0o700); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, lock)
	serviceB := New(root)
	serviceB.SetTestBackend(nil, lock)
	job, err := serviceA.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := serviceB.Reconcile(); err != nil {
		t.Fatal(err)
	}
	current, err := serviceA.Job(job.ID)
	if err != nil || current.Status != statusRunning {
		t.Fatalf("live job changed by second service: job=%+v err=%v", current, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".kura-backup-live")); err != nil {
		t.Fatalf("live staging was removed: %v", err)
	}
	if _, err := serviceA.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, err = serviceA.Job(job.ID)
		if err == nil && current.Status == statusCanceled {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("active job did not finish cancellation: job=%+v err=%v", current, err)
}

func TestJobStateRejectsSymlinkPathsAndUsesPrivateModes(t *testing.T) {
	root := t.TempDir()
	service := New(root)
	job := Job{ID: "backup-safe", Status: statusRunning}
	if err := service.writeJob(root, job); err != nil {
		t.Fatal(err)
	}
	dir := service.jobDir(root)
	if info, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("job directory is not private: mode=%v", info.Mode().Perm())
	}
	path := service.jobPath(root, job.ID)
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("job state is not private: mode=%v", info.Mode().Perm())
	}

	other := t.TempDir()
	target := filepath.Join(other, "target.json")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := service.writeJob(root, Job{ID: job.ID, Status: statusFailed}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink job file err=%v", err)
	}
	if _, err := service.readJob(root, job.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("read symlink job file err=%v", err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "untouched" {
		t.Fatalf("symlink target changed: %q err=%v", body, err)
	}
}

func TestJobDirectoryAndCancelMarkerRejectSymlinks(t *testing.T) {
	root := t.TempDir()
	service := New(root)
	other := t.TempDir()
	if err := os.Symlink(other, service.jobDir(root)); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if err := service.writeJob(root, Job{ID: "backup-dir", Status: statusRunning}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink job directory err=%v", err)
	}
	if _, err := service.readJob(root, "backup-dir"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("read symlink job directory err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "backup-dir.json")); !os.IsNotExist(err) {
		t.Fatal("write through job directory symlink")
	}

	if err := os.Remove(service.jobDir(root)); err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "backup-cancel", Status: statusRunning}
	if err := service.writeJob(root, job); err != nil {
		t.Fatal(err)
	}
	markerTarget := filepath.Join(other, "marker")
	if err := os.WriteFile(markerTarget, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(markerTarget, service.cancelPath(root, job.ID)); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	if _, err := service.Cancel(job.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink cancel marker err=%v", err)
	}
	if body, err := os.ReadFile(markerTarget); err != nil || string(body) != "untouched" {
		t.Fatalf("cancel marker target changed: %q err=%v", body, err)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type regularFileInfo struct{}

func (regularFileInfo) Name() string       { return "online_backup_proof.py" }
func (regularFileInfo) Size() int64        { return 1 }
func (regularFileInfo) Mode() os.FileMode  { return 0o644 }
func (regularFileInfo) ModTime() time.Time { return time.Time{} }
func (regularFileInfo) IsDir() bool        { return false }
func (regularFileInfo) Sys() any           { return nil }

func TestOnlineBackupScriptUsesPackageBesideExecutable(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	scriptDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "online_backup_proof.py")
	if err := os.WriteFile(script, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := onlineBackupScript(filepath.Join(binDir, "kura-server"), "", os.Stat)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(script) {
		t.Fatalf("script=%s want %s", got, script)
	}
}

func TestOnlineBackupScriptOverrideDoesNotSearchElsewhere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.py")
	if _, err := onlineBackupScript(filepath.Join("bin", "kura-server"), missing, os.Stat); err == nil {
		t.Fatal("missing override was accepted")
	}
}

func TestOnlineBackupScriptFallsBackToSourceCheckout(t *testing.T) {
	got, err := onlineBackupScript(filepath.Join("elsewhere", "kura-server"), "", func(path string) (os.FileInfo, error) {
		if filepath.ToSlash(path) == "scripts/online_backup_proof.py" {
			return regularFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	})
	if err != nil || filepath.ToSlash(got) != "scripts/online_backup_proof.py" {
		t.Fatalf("script=%s err=%v", got, err)
	}
}
