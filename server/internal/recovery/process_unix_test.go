//go:build !windows

package recovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCancelStopsBackupChildTools(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("Python is unavailable")
	}
	root := t.TempDir()
	script := filepath.Join(root, "slow_backup.py")
	// Model a pg_dump child that must receive cancellation along with Python.
	body := `
import pathlib, subprocess, sys, time
def open_postgres_snapshot(*args, **kwargs):
    return None
def run_online_backup(media, output, opener):
    child = """
import pathlib, signal, sys, time
root = pathlib.Path(sys.argv[1])
def stop(signum, frame):
    root.with_suffix('.stopped').write_text('stopped')
    sys.exit(0)
signal.signal(signal.SIGTERM, stop)
root.with_suffix('.ready').write_text('ready')
time.sleep(60)
"""
    subprocess.Popen([sys.executable, '-c', child, output])
    time.sleep(60)
`
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KURA_ONLINE_BACKUP_SCRIPT", script)
	output := filepath.Join(root, "attempt")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- defaultBackupRun(ctx, os.Environ(), root, output) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(output + ".ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("backup child did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled backup reported success")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("backup tools outlived cancellation")
	}
	if _, err := os.Stat(output + ".stopped"); err != nil {
		t.Fatal("backup child did not receive cancellation")
	}
}

func TestBackupConnectionQueryOptionsMatchLibpq(t *testing.T) {
	env, err := pgProcessEnv("postgresql://authority-user:secret@127.0.0.1/library?host=localhost&user=query-user&sslrootcert=%2Ftmp%2Ftest-ca.pem&connect_timeout=5")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PGHOST": "localhost", "PGUSER": "query-user", "PGSSLROOTCERT": "/tmp/test-ca.pem", "PGCONNECT_TIMEOUT": "5"}
	for key, expected := range want {
		count := 0
		for _, entry := range env {
			if strings.HasPrefix(entry, key+"=") {
				count++
				if entry != key+"="+expected {
					t.Fatalf("wrong %s value", key)
				}
			}
		}
		if count != 1 {
			t.Fatalf("expected one %s entry, got %d", key, count)
		}
	}
	for _, url := range []string{"postgresql://localhost/library?host=a&host=b", "postgresql://localhost/library?unsupported=1"} {
		if _, err := pgProcessEnv(url); err == nil {
			t.Fatal("ambiguous or unsupported connection option accepted")
		}
	}
}
