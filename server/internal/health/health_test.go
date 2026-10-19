package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
)

type memReader struct {
	originals []Original
	key       string
	page      Page
	counts    Counts
}

func (m memReader) Counts(context.Context) (Counts, error) { return m.counts, nil }
func (m memReader) Originals(context.Context, int) ([]Original, error) {
	return m.originals, nil
}
func (m memReader) OriginalKey(context.Context, string) (string, error) { return m.key, nil }
func (m memReader) MissingThumbnails(context.Context, int) (Page, error) {
	return m.page, nil
}

type memRepair struct {
	ids []string
	id  string
}

func (m *memRepair) RepairThumbnail(_ context.Context, postID, _ string) (string, string, error) {
	m.ids = append(m.ids, postID)
	return m.id, "pending", nil
}

type listingBackup struct{}

func (listingBackup) List(context.Context) (recovery.Listing, error) {
	return recovery.Listing{Configured: true, Backups: []recovery.Backup{
		{ID: "nightly", CreatedAt: "2026-10-15T00:00:00Z", Status: "available"},
		{ID: "older", CreatedAt: "2026-10-14T00:00:00Z", Status: "invalid"},
	}}, nil
}

func TestSummaryLeavesStorageUnknownUntilScanAndHidesPaths(t *testing.T) {
	svc := New(memReader{counts: Counts{MissingThumbnails: 2, FailedThumbnails: 1, Backlog: 4}}, nil, nil, listingBackup{}, MediaLocal)
	summary, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.MissingOriginals.Known || summary.StorageBytes.Known {
		t.Fatalf("storage facts must stay unknown before a scan: %+v", summary)
	}
	if !summary.MissingThumbnails.Known || summary.MissingThumbnails.Value != 2 || summary.ProcessingBacklog.Value != 4 {
		t.Fatalf("cheap counts = %+v", summary)
	}
	if summary.LatestVerifiedBackup == nil || summary.LatestVerifiedBackup.ID != "nightly" {
		t.Fatalf("backup = %+v", summary.LatestVerifiedBackup)
	}
	encoded := summary.LatestVerifiedBackup.ID + summary.Media
	if strings.Contains(encoded, `C:\`) || strings.Contains(encoded, "/var/") {
		t.Fatal("summary exposed a path")
	}
}

func TestS3ScanIsUnsupported(t *testing.T) {
	svc := New(memReader{}, LocalFiles{Root: t.TempDir()}, nil, nil, MediaS3)
	if err := svc.StartScan(); err != ErrUnsupported {
		t.Fatalf("err = %v", err)
	}
	summary, err := svc.Summary(context.Background())
	if err != nil || summary.S3Checks != "unsupported" {
		t.Fatalf("summary = %+v err %v", summary, err)
	}
}

func TestScanCountsMissingAndEmptyFixtures(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "ok.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "empty.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	reader := memReader{originals: []Original{
		{PostID: "1", Key: "uploads/ok.png"},
		{PostID: "2", Key: "uploads/empty.png"},
		{PostID: "3", Key: "uploads/missing.png"},
	}}
	svc := New(reader, LocalFiles{Root: root}, nil, nil, MediaLocal)
	if err := svc.StartScan(); err != nil {
		t.Fatal(err)
	}
	if err := svc.WaitScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	scan := svc.ScanStatus()
	if scan.Status != ScanCompleted || scan.Checked != 3 || scan.Missing != 1 || scan.Corrupt != 1 || scan.Bytes != 3 {
		t.Fatalf("scan = %+v", scan)
	}
	if strings.Contains(scan.Error, "uploads") || strings.Contains(scan.Error, root) {
		t.Fatalf("scan leaked a path: %+v", scan)
	}
	summary, err := svc.Summary(context.Background())
	if err != nil || !summary.MissingOriginals.Known || summary.MissingOriginals.Value != 1 || summary.StorageBytes.Value != 3 {
		t.Fatalf("summary = %+v err %v", summary, err)
	}
}

func TestCancelStopsTheScan(t *testing.T) {
	blocked := make(chan struct{}, 1)
	svc := New(memReader{originals: []Original{{PostID: "1", Key: "uploads/a.png"}, {PostID: "2", Key: "uploads/b.png"}}}, blockingPresence{wait: blocked}, nil, nil, MediaLocal)
	if err := svc.StartScan(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not reach the first object")
	}
	if err := svc.CancelScan(); err != nil {
		t.Fatal(err)
	}
	if err := svc.WaitScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.ScanStatus().Status != ScanCanceled {
		t.Fatalf("status = %+v", svc.ScanStatus())
	}
}

type blockingPresence struct{ wait chan struct{} }

func (b blockingPresence) Present(ctx context.Context, _ string) (int64, bool, error) {
	select {
	case b.wait <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return 0, false, ctx.Err()
}

func TestDuplicateRepairReturnsTheSameJob(t *testing.T) {
	repair := &memRepair{id: "job-7"}
	svc := New(memReader{key: "uploads/a.png"}, nil, repair, nil, MediaLocal)
	first, status, err := svc.Repair(context.Background(), "42")
	if err != nil || first != "job-7" || status != "pending" {
		t.Fatalf("first = %s %s %v", first, status, err)
	}
	second, _, err := svc.Repair(context.Background(), "42")
	if err != nil || second != first || len(repair.ids) != 2 {
		t.Fatalf("second = %s calls %v err %v", second, repair.ids, err)
	}
	if _, _, err := svc.Repair(context.Background(), "../etc"); err != ErrBadPost {
		t.Fatalf("bad id err = %v", err)
	}
}

func TestTraversalKeyIsNotPresent(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "secret.png")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	_, ok, err := (LocalFiles{Root: root}).Present(context.Background(), "../secret.png")
	if err != nil || ok {
		t.Fatalf("traversal ok=%v err=%v", ok, err)
	}
}
