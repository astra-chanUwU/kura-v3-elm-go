// Package folderimport polls one configured directory and imports stable
// files through the existing library import API. It never deletes or moves
// a source file, and it does not start unless a caller asks it to run.
package folderimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxFileBytes = 25 << 20
	version      = 1
)

// Importer is the library API used by an explicit import pass.
type Importer interface {
	Identity(context.Context) (account, library string, err error)
	Duplicates(context.Context, []string) (map[string]string, error)
	Import(context.Context, ImportRequest) (ImportResult, error)
}

// ImportRequest is one stable file. Open is called only when a write is attempted.
type ImportRequest struct {
	Rel          string
	Open         func() (io.ReadCloser, error)
	ExpectedHash string
	Size         int64
	CollectionID string
	Tags         []string
	Artist       string
	Source       string
}

// ImportResult is a durable receipt or an uncertain write.
type ImportResult struct {
	PostID    string
	Duplicate bool
	Uncertain bool
}

// Options configures one explicit pass or a poll loop.
type Options struct {
	Root         string
	Checkpoint   string
	Apply        bool
	CollectionID string
	Tags         []string
	Artist       string
	Source       string
	Settle       time.Duration
	Interval     time.Duration
	Sleep        func(time.Duration)
	Importer     Importer
	Stdout       io.Writer
}

// File is one checkpointed source. Credentials are never stored here.
type File struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
	PostID string `json:"post_id,omitempty"`
}

// Checkpoint is scoped to one library target and account.
type Checkpoint struct {
	Version int             `json:"version"`
	Root    string          `json:"root"`
	Account string          `json:"account"`
	Library string          `json:"library"`
	Files   map[string]File `json:"files"`
}

type observation struct {
	rel  string
	size int64
	mod  int64
	path string
}

// Run performs a dry-run unless Apply is set. Interval enables polling.
func Run(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Sleep == nil {
		opts.Sleep = time.Sleep
	}
	if opts.Settle <= 0 {
		opts.Settle = time.Second
	}
	root, err := canonicalDir(opts.Root)
	if err != nil {
		return err
	}
	if opts.Apply && strings.TrimSpace(opts.Checkpoint) == "" {
		return errors.New("a checkpoint path is required when applying")
	}
	if opts.Checkpoint != "" {
		checkpoint, err := filepath.Abs(opts.Checkpoint)
		if err != nil {
			return errors.New("checkpoint path is invalid")
		}
		opts.Checkpoint = checkpoint
		if inside(root, checkpoint) {
			return errors.New("checkpoint must stay outside the watched directory")
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := pass(ctx, root, opts); err != nil {
			return err
		}
		if opts.Interval <= 0 {
			return nil
		}
		opts.Sleep(opts.Interval)
	}
}

func pass(ctx context.Context, root string, opts Options) error {
	first, err := scan(root, opts.Checkpoint)
	if err != nil {
		return err
	}
	opts.Sleep(opts.Settle)
	second, err := scan(root, opts.Checkpoint)
	if err != nil {
		return err
	}
	var cp Checkpoint
	var account, library string
	trusted := false
	if opts.Apply {
		account, library, err = opts.Importer.Identity(ctx)
		if err != nil {
			return err
		}
		if account == "" || library == "" || strings.Contains(account, "://") {
			return errors.New("library identity is unavailable")
		}
		cp, trusted, err = loadCheckpoint(opts.Checkpoint, root, account, library)
		if err != nil {
			return err
		}
		if !trusted {
			fmt.Fprintln(opts.Stdout, "checkpoint belongs to another account or library and was not used")
			cp = Checkpoint{Version: version, Root: root, Account: account, Library: library, Files: map[string]File{}}
		}
	}
	byHash := map[string]string{}
	for rel, file := range cp.Files {
		if file.SHA256 != "" && (file.Status == "imported" || file.Status == "duplicate") {
			byHash[file.SHA256] = rel
		}
	}
	for rel, cur := range second {
		prev, seen := first[rel]
		if !seen || prev.size != cur.size || prev.mod != cur.mod {
			fmt.Fprintf(opts.Stdout, "unstable\t%s\n", rel)
			continue
		}
		if cur.size <= 0 || cur.size > MaxFileBytes {
			fmt.Fprintf(opts.Stdout, "skipped\t%s\tsize\n", rel)
			continue
		}
		sum, err := hashFile(cur.path, cur.size)
		if err != nil {
			fmt.Fprintf(opts.Stdout, "unreadable\t%s\n", rel)
			continue
		}
		if !opts.Apply {
			fmt.Fprintf(opts.Stdout, "preview\t%s\t%s\n", rel, sum)
			continue
		}
		if existing, ok := cp.Files[rel]; ok && existing.SHA256 == sum && (existing.Status == "imported" || existing.Status == "duplicate") {
			fmt.Fprintf(opts.Stdout, "already\t%s\n", rel)
			continue
		}
		if previousRel, ok := byHash[sum]; ok && previousRel != rel {
			cp.Files[rel] = cp.Files[previousRel]
			delete(cp.Files, previousRel)
			byHash[sum] = rel
			if err := saveCheckpoint(opts.Checkpoint, cp); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "renamed\t%s\n", rel)
			continue
		}
		if existing, ok := cp.Files[rel]; ok && existing.Status == "uncertain" && existing.SHA256 == sum {
			found, err := lookup(ctx, opts.Importer, sum)
			if err != nil {
				fmt.Fprintf(opts.Stdout, "pending\t%s\n", rel)
				continue
			}
			if postID, ok := found[sum]; ok {
				cp.Files[rel] = File{SHA256: sum, Size: cur.size, Status: "imported", PostID: postID}
				byHash[sum] = rel
				if err := saveCheckpoint(opts.Checkpoint, cp); err != nil {
					return err
				}
				fmt.Fprintf(opts.Stdout, "imported\t%s\n", rel)
				continue
			}
		}
		found, err := lookup(ctx, opts.Importer, sum)
		if err != nil {
			fmt.Fprintf(opts.Stdout, "pending\t%s\n", rel)
			continue
		}
		if postID, ok := found[sum]; ok {
			cp.Files[rel] = File{SHA256: sum, Size: cur.size, Status: "duplicate", PostID: postID}
			byHash[sum] = rel
			if err := saveCheckpoint(opts.Checkpoint, cp); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "duplicate\t%s\n", rel)
			continue
		}
		result, err := opts.Importer.Import(ctx, ImportRequest{
			Rel: rel, ExpectedHash: sum, Size: cur.size,
			CollectionID: opts.CollectionID, Tags: opts.Tags, Artist: opts.Artist, Source: opts.Source,
			Open: func() (io.ReadCloser, error) { return os.Open(cur.path) },
		})
		if err != nil || result.Uncertain || result.PostID == "" && !result.Duplicate {
			cp.Files[rel] = File{SHA256: sum, Size: cur.size, Status: "uncertain"}
			_ = saveCheckpoint(opts.Checkpoint, cp)
			fmt.Fprintf(opts.Stdout, "uncertain\t%s\n", rel)
			continue
		}
		status := "imported"
		if result.Duplicate {
			status = "duplicate"
		}
		cp.Files[rel] = File{SHA256: sum, Size: cur.size, Status: status, PostID: result.PostID}
		byHash[sum] = rel
		if err := saveCheckpoint(opts.Checkpoint, cp); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "%s\t%s\n", status, rel)
	}
	return nil
}

func lookup(ctx context.Context, importer Importer, sum string) (map[string]string, error) {
	if importer == nil {
		return nil, errors.New("import API is unavailable")
	}
	return importer.Duplicates(ctx, []string{sum})
}

func scan(root, checkpoint string) (map[string]observation, error) {
	out := map[string]observation{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel) || path == checkpoint {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || !supportedExt(rel) {
			return nil
		}
		out[rel] = observation{rel: rel, size: info.Size(), mod: info.ModTime().UnixNano(), path: path}
		return nil
	})
	return out, err
}

func excluded(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "" || strings.HasPrefix(part, ".") || part == "__MACOSX" || strings.HasSuffix(strings.ToLower(part), ".app") {
			return true
		}
		if part == "kura-watch-checkpoint.json" {
			return true
		}
	}
	return false
}

func supportedExt(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".mp4", ".webm":
		return true
	default:
		return false
	}
}

func hashFile(path string, size int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != size || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("file changed")
	}
	sum := sha256.New()
	n, err := io.Copy(sum, file)
	if err != nil || n != size {
		return "", errors.New("file changed")
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

func canonicalDir(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("a watched directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", errors.New("watched directory is invalid")
	}
	abs = filepath.Clean(abs)
	if isRoot(abs) {
		return "", errors.New("refusing to watch a filesystem root")
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("watched directory must be a real directory")
	}
	return abs, nil
}

func isRoot(path string) bool {
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) {
		return true
	}
	vol := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, vol)
	return rest == "" || rest == `\` || rest == "/"
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func loadCheckpoint(path, root, account, library string) (Checkpoint, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Checkpoint{Version: version, Root: root, Account: account, Library: library, Files: map[string]File{}}, true, nil
	}
	if err != nil {
		return Checkpoint{}, false, errors.New("checkpoint is unreadable")
	}
	var cp Checkpoint
	if json.Unmarshal(data, &cp) != nil || cp.Version != version || cp.Files == nil {
		return Checkpoint{}, false, errors.New("checkpoint is unusable")
	}
	if cp.Root != root || cp.Account != account || cp.Library != library {
		return cp, false, nil
	}
	return cp, true, nil
}

func saveCheckpoint(path string, cp Checkpoint) error {
	if cp.Files == nil {
		cp.Files = map[string]File{}
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return errors.New("checkpoint could not be saved")
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".kura-watch-*")
	if err != nil {
		return errors.New("checkpoint could not be saved")
	}
	name := temp.Name()
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(name)
		return errors.New("checkpoint could not be saved")
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(path)
		if err := os.Rename(name, path); err != nil {
			_ = os.Remove(name)
			return errors.New("checkpoint could not be saved")
		}
	}
	return nil
}
