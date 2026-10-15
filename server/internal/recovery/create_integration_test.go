package recovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"
)

func TestRealCreatorLockAndFailedPublication(t *testing.T) {
	adminURL := os.Getenv("KURA_BACKUP_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Skip("set an explicitly disposable local admin URL to run backup creation")
	}
	for _, tool := range []string{"psql", "pg_dump"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not on PATH")
		}
	}
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python is not on PATH")
		}
	}
	parsed, err := url.Parse(adminURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("admin URL is not a local postgres URL")
	}
	switch parsed.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		t.Fatal("backup creation drill is restricted to local PostgreSQL")
	}

	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "kura_s2bak_" + hex.EncodeToString(nonce[:])
	if !regexp.MustCompile(`^kura_s2bak_[0-9a-f]{8}$`).MatchString(name) {
		t.Fatal("refusing database name")
	}
	adminEnv, err := pgProcessEnv(adminURL)
	if err != nil {
		t.Fatal("could not prepare disposable database environment")
	}
	psqlExec(t, adminEnv, "CREATE DATABASE "+name)
	t.Cleanup(func() {
		psqlExec(t, adminEnv, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	parsed.Path = "/" + name
	parsed.RawPath = ""
	databaseURL := parsed.String()
	dbEnv, err := pgProcessEnv(databaseURL)
	if err != nil {
		t.Fatal("could not prepare disposable database environment")
	}
	rootDir := filepath.Clean(filepath.Join("..", "..", ".."))
	migrations, err := filepath.Glob(filepath.Join(rootDir, "db", "migrations", "*.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatal("migrations are unavailable")
	}
	for _, migration := range migrations {
		psqlFile(t, dbEnv, migration)
	}

	for _, key := range remoteSettingKeys {
		t.Setenv(key, "")
	}
	script := filepath.Join(rootDir, "scripts", "online_backup_proof.py")
	if _, err := os.Stat(script); err != nil {
		t.Fatal("backup script is unavailable")
	}
	t.Setenv("KURA_ONLINE_BACKUP_SCRIPT", script)
	mediaRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIA_ROOT", mediaRoot)
	t.Setenv("DATABASE_URL", databaseURL)

	release, err := holdBackupLock(context.Background(), dbEnv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holdBackupLock(context.Background(), dbEnv); !errors.Is(err, ErrActive) {
		release()
		t.Fatalf("second lock err=%v", err)
	}
	release()

	root := t.TempDir()
	service := New(root)
	service.SetTestBackend(func(ctx context.Context, env []string, media, output string) error {
		err := defaultBackupRun(ctx, env, media, output)
		if err != nil {
			t.Logf("backup tool failed: %s", redactTool(err.Error()))
		}
		return err
	}, nil)
	job := waitBackup(t, service)
	if job.Status != statusSucceeded {
		if _, err := os.Lstat(filepath.Join(root, job.ID)); !os.IsNotExist(err) {
			t.Fatal("failed attempt published a directory")
		}
	}
	listing, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if job.Status != statusFailed || job.Error != "atomic publication is unavailable on this platform" || len(listing.Backups) != 0 {
			t.Fatalf("job=%+v listing=%+v", job, listing.Backups)
		}
	} else if job.Status != statusSucceeded || len(listing.Backups) != 1 {
		t.Fatalf("job=%+v listing=%+v", job, listing.Backups)
	} else if _, err := service.Verify(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}

	t.Run("cancel releases snapshot and subprocesses", func(t *testing.T) {
		original, err := filepath.Abs(script)
		if err != nil {
			t.Fatal(err)
		}
		wrapper := filepath.Join(t.TempDir(), "slow_creator.py")
		body := fmt.Sprintf(`
import importlib.util, pathlib, time
spec = importlib.util.spec_from_file_location("real_proof", %q)
real = importlib.util.module_from_spec(spec)
spec.loader.exec_module(real)
open_postgres_snapshot = real.open_postgres_snapshot
def run_online_backup(media, output, opener):
    def pause(ready):
        pathlib.Path(output + ".ready").write_text("ready")
        time.sleep(60)
    return real.run_online_backup(media, output, opener, after_ready=pause)
`, original)
		if err := os.WriteFile(wrapper, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KURA_ONLINE_BACKUP_SCRIPT", wrapper)
		cancelService := New(root)
		attempt, err := cancelService.Start(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ready := filepath.Join(root, attempt.ID+".ready")
		t.Cleanup(func() { _ = os.Remove(ready) })
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_, _ = cancelService.Cancel(attempt.ID)
				t.Fatal("snapshot was not opened")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if _, err := cancelService.Cancel(attempt.ID); err != nil {
			t.Fatal(err)
		}
		deadline = time.Now().Add(10 * time.Second)
		for {
			job, err := cancelService.Job(attempt.ID)
			if err == nil && job.Status == statusCanceled {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("cancellation did not finish: %+v %v", job, err)
			}
			time.Sleep(20 * time.Millisecond)
		}
		if _, err := os.Lstat(filepath.Join(root, attempt.ID)); !os.IsNotExist(err) {
			t.Fatal("cancellation published a backup")
		}
		// Both the outer creator lock and inner exported snapshot must close.
		released, err := holdBackupLock(context.Background(), dbEnv)
		if err != nil {
			t.Fatal("cancellation retained the creation lock")
		}
		released()
		psqlExec(t, dbEnv, `DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid())
			THEN RAISE EXCEPTION 'snapshot backend survived cancellation'; END IF;
		END $$`)
	})

	psqlExec(t, dbEnv, `INSERT INTO posts (preview_url, original_url, media_type, width, height, hash, file_size)
		VALUES ('/media/missing.png', '/media/missing.png', 'image/png', 1, 1, 'sha256:0000000000000000000000000000000000000000000000000000000000000000', 1)`)
	missing := New(root)
	failed := waitBackup(t, missing)
	if failed.Status != statusFailed || failed.Error != "referenced media is missing" {
		t.Fatalf("job=%+v", failed)
	}
	if _, err := os.Lstat(filepath.Join(root, failed.ID)); !os.IsNotExist(err) {
		t.Fatal("missing media published a directory")
	}
}

func waitBackup(t *testing.T, service *Service) Job {
	t.Helper()
	var job Job
	var err error
	deadline := time.Now().Add(20 * time.Second)
	for {
		job, err = service.Start(context.Background())
		if err == nil || !errors.Is(err, ErrActive) || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	id := job.ID
	deadline = time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		job, err = service.Job(id)
		if err == nil && job.Status != statusRunning {
			return job
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("job=%+v err=%v", job, err)
	return Job{}
}

func psqlExec(t *testing.T, env []string, sql string) {
	t.Helper()
	cmd := exec.Command("psql", "-X", "-v", "ON_ERROR_STOP=1", "-c", sql)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("disposable database command failed: %s", redactTool(string(out)))
	}
}

func psqlFile(t *testing.T, env []string, path string) {
	t.Helper()
	cmd := exec.Command("psql", "-X", "-v", "ON_ERROR_STOP=1", "-f", path)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("disposable migration failed: %s", redactTool(string(out)))
	}
}
