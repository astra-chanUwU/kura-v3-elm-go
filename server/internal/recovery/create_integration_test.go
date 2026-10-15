package recovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	t.Setenv("MEDIA_ROOT", t.TempDir())
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
