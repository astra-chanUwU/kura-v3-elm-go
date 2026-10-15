package folderimport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDryRunLeavesSourcesAndSkipsHiddenPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.png"), []byte("png-a"))
	writeFile(t, filepath.Join(root, ".hidden.png"), []byte("secret"))
	writeFile(t, filepath.Join(root, "__MACOSX", "inside.png"), []byte("package"))
	before := readTree(t, root)
	var out bytes.Buffer
	err := Run(context.Background(), Options{Root: root, Settle: time.Millisecond, Sleep: func(time.Duration) {}, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readTree(t, root), before) {
		t.Fatal("dry-run changed the source directory")
	}
	text := out.String()
	if !strings.Contains(text, "preview\tphoto.png\tsha256:") || strings.Contains(text, ".hidden") || strings.Contains(text, "__MACOSX") || strings.Contains(text, "secret") {
		t.Fatalf("preview = %s", text)
	}
}

func TestUnstablePartialCopyIsNotHashed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "partial.png")
	writeFile(t, path, []byte("ab"))
	calls := 0
	var out bytes.Buffer
	err := Run(context.Background(), Options{
		Root: root, Settle: time.Millisecond,
		Sleep: func(time.Duration) {
			calls++
			if calls == 1 {
				writeFile(t, path, []byte("abcdef"))
			}
		},
		Stdout: &out,
	})
	if err != nil || !strings.Contains(out.String(), "unstable\tpartial.png") || strings.Contains(out.String(), "preview") {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestApplyRecordsDuplicateWithoutSecondUpload(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.png"), []byte("png-a"))
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.json")
	api := &fakeAPI{duplicates: map[string]string{}}
	var out bytes.Buffer
	opts := Options{Root: root, Checkpoint: checkpoint, Apply: true, Settle: time.Millisecond, Sleep: func(time.Duration) {}, Importer: api, Stdout: &out}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if api.imports != 1 || !strings.Contains(out.String(), "imported\tphoto.png") {
		t.Fatalf("imports=%d out=%s", api.imports, out.String())
	}
	out.Reset()
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if api.imports != 1 || !strings.Contains(out.String(), "already\tphoto.png") {
		t.Fatalf("second imports=%d out=%s", api.imports, out.String())
	}
}

func TestCrashAfterReceiptDoesNotUploadAgain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.png"), []byte("png-b"))
	api := &fakeAPI{duplicates: map[string]string{}}
	missing := filepath.Join(t.TempDir(), "missing", "checkpoint.json")
	opts := Options{Root: root, Checkpoint: missing, Apply: true, Settle: time.Millisecond, Sleep: func(time.Duration) {}, Importer: api, Stdout: ioDiscard{}}
	if err := Run(context.Background(), opts); err == nil || api.imports != 1 {
		t.Fatalf("err=%v imports=%d", err, api.imports)
	}
	opts.Checkpoint = filepath.Join(t.TempDir(), "checkpoint.json")
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if api.imports != 1 {
		t.Fatalf("retry imports=%d", api.imports)
	}
}

func TestAccountChangeDoesNotTrustCheckpoint(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.png"), []byte("png-c"))
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.json")
	api := &fakeAPI{account: "user-1", duplicates: map[string]string{}}
	opts := Options{Root: root, Checkpoint: checkpoint, Apply: true, Settle: time.Millisecond, Sleep: func(time.Duration) {}, Importer: api, Stdout: ioDiscard{}}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	api.account = "user-2"
	api.duplicates = map[string]string{}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if api.imports != 2 {
		t.Fatalf("imports=%d", api.imports)
	}
}

func TestUnavailableAPIDoesNotClaimImported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.png"), []byte("png-d"))
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.json")
	api := &fakeAPI{down: true}
	var out bytes.Buffer
	err := Run(context.Background(), Options{Root: root, Checkpoint: checkpoint, Apply: true, Settle: time.Millisecond, Sleep: func(time.Duration) {}, Importer: api, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if api.imports != 0 || !strings.Contains(out.String(), "pending\tphoto.png") {
		t.Fatalf("imports=%d out=%s", api.imports, out.String())
	}
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatal("unavailable API wrote a checkpoint")
	}
}

func TestRenameKeepsTheReceipt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "photo.png")
	writeFile(t, path, []byte("png-e"))
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.json")
	api := &fakeAPI{duplicates: map[string]string{}}
	opts := Options{Root: root, Checkpoint: checkpoint, Apply: true, Settle: time.Millisecond, Sleep: func(time.Duration) {}, Importer: api, Stdout: ioDiscard{}}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(root, "renamed.png")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts.Stdout = &out
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if api.imports != 1 || !strings.Contains(out.String(), "renamed\trenamed.png") {
		t.Fatalf("imports=%d out=%s", api.imports, out.String())
	}
}

type fakeAPI struct {
	account    string
	library    string
	imports    int
	duplicates map[string]string
	down       bool
}

func (f *fakeAPI) Identity(context.Context) (string, string, error) {
	if f.account == "" {
		f.account = "user-1"
	}
	if f.library == "" {
		f.library = "http://127.0.0.1:8080"
	}
	return f.account, f.library, nil
}

func (f *fakeAPI) Duplicates(_ context.Context, hashes []string) (map[string]string, error) {
	if f.down {
		return nil, os.ErrClosed
	}
	out := map[string]string{}
	for _, hash := range hashes {
		if id, ok := f.duplicates[hash]; ok {
			out[hash] = id
		}
	}
	return out, nil
}

func (f *fakeAPI) Import(_ context.Context, request ImportRequest) (ImportResult, error) {
	if f.down {
		return ImportResult{}, os.ErrClosed
	}
	body, err := request.Open()
	if err != nil {
		return ImportResult{}, err
	}
	defer body.Close()
	f.imports++
	f.duplicates[request.ExpectedHash] = "post-1"
	return ImportResult{PostID: "post-1"}, nil
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTree(t *testing.T, root string) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		buf.WriteString(path)
		buf.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
