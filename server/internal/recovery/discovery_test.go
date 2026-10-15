package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDiscoversNewestPersistedRunningJob(t *testing.T) {
	root := t.TempDir()
	writer := New(root)
	for _, job := range []Job{
		{ID: "backup-20261015T120000Z-aabbccdd", Status: statusSucceeded},
		{ID: "backup-20261015T120100Z-aabbccdd", Status: statusRunning},
		{ID: "backup-20261015T120200Z-aabbccdd", Status: statusFailed},
	} {
		if err := writer.writeJob(root, job); err != nil {
			t.Fatal(err)
		}
	}

	// A fresh service has no in-memory active ID; it must reattach from disk.
	listing, err := New(root).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listing.ActiveJob == nil || listing.ActiveJob.ID != "backup-20261015T120100Z-aabbccdd" || listing.ActiveJob.Status != statusRunning {
		t.Fatalf("active job=%+v", listing.ActiveJob)
	}
	body, err := json.Marshal(listing)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "" || strings.Contains(string(body), root) {
		t.Fatalf("listing exposed a filesystem path: %s", body)
	}
}

func TestListOmitsActiveJobWhenAllPersistedJobsAreTerminal(t *testing.T) {
	root := t.TempDir()
	service := New(root)
	for _, status := range []string{statusSucceeded, statusFailed, statusCanceled} {
		if err := service.writeJob(root, Job{ID: "backup-" + status, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	listing, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listing.ActiveJob != nil {
		t.Fatalf("terminal job was returned as active: %+v", listing.ActiveJob)
	}
}

func TestListAcceptsAnEmptyPersistedJobDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, jobDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	listing, err := New(root).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listing.ActiveJob != nil || listing.Backups == nil {
		t.Fatalf("listing=%+v", listing)
	}
}

func TestListFailsClosedWhenJobHistoryExceedsBound(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, jobDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxJobHistory; i++ {
		id := fmt.Sprintf("backup-%04d", i)
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(`{"id":"`+id+`","status":"failed"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := New(root).List(context.Background())
	if !errors.Is(err, ErrJobHistoryTooLarge) {
		t.Fatalf("overflow err=%v, want ErrJobHistoryTooLarge", err)
	}
}
