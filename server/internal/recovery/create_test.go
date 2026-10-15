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
	service := New(root)
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
