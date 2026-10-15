// Package recovery lists, verifies, creates, and manages local backups in the
// format written by scripts/library-backup.py. Restore remains an offline
// operator action.
package recovery

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxBackups       = 100
	MaxManifestBytes = 16 << 20
	maxManifestFiles = 100000
	hashChunk        = 1 << 20
)

var (
	ErrUnconfigured = errors.New("backup root is not configured")
	ErrBusy         = errors.New("a backup verification is already running")
	ErrActive       = errors.New("a backup is already running")
	ErrUnsupported  = errors.New("S3 backups are not supported")
	ErrNotFound     = errors.New("backup not found")
	ErrInvalid      = errors.New("backup is invalid")
	ErrCanceled     = errors.New("backup verification canceled")
	ErrTimeout      = errors.New("backup verification timed out")
	ErrShuttingDown = errors.New("backup service is shutting down")
	backupID        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type Backup struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	MediaCount int    `json:"media_count"`
	Bytes      int64  `json:"bytes"`
	Status     string `json:"status"`
}
type Listing struct {
	Configured bool     `json:"configured"`
	Backups    []Backup `json:"backups"`
	Truncated  bool     `json:"truncated"`
	ActiveJob  *Job     `json:"active_job,omitempty"`
}
type Receipt struct {
	ID         string `json:"id"`
	VerifiedAt string `json:"verified_at"`
	MediaCount int    `json:"media_count"`
	Bytes      int64  `json:"bytes"`
}
type manifest struct {
	Version   int         `json:"version"`
	CreatedAt string      `json:"created_at"`
	Files     []fileEntry `json:"files"`
}
type fileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Service struct {
	root         string
	verifying    atomic.Bool
	creating     atomic.Bool
	now          func() time.Time
	run          backupRunner
	lock         lockHolder
	mu           sync.Mutex
	active       string
	cancel       context.CancelFunc
	activeDone   chan struct{}
	lifecycle    context.Context
	lifeCancel   context.CancelFunc
	shuttingDown bool
	maintenance  MaintenanceConfig
}

// New reads only the operator-provided KURA_BACKUP_ROOT value. The API never
// accepts or returns filesystem paths.
func New(root string) *Service {
	lifecycle, cancel := context.WithCancel(context.Background())
	return &Service{root: strings.TrimSpace(root), now: time.Now, lifecycle: lifecycle, lifeCancel: cancel}
}
func NewFromEnv() *Service { return New(os.Getenv("KURA_BACKUP_ROOT")) }

// Shutdown rejects new backups, cancels the service lifetime, and waits for an
// in-flight attempt to finish cleanup and release its advisory lock.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.shuttingDown = true
	lifeCancel := s.lifeCancel
	activeCancel := s.cancel
	done := s.activeDone
	s.mu.Unlock()
	if lifeCancel != nil {
		lifeCancel()
	}
	if activeCancel != nil {
		activeCancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) List(ctx context.Context) (Listing, error) {
	result := Listing{Backups: []Backup{}}
	root, err := s.realRoot()
	if errors.Is(err, ErrUnconfigured) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Configured = true
	result.ActiveJob, err = s.discoverActiveJob(ctx, root)
	if err != nil {
		return result, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return result, fmt.Errorf("read backup root: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !backupID.MatchString(entry.Name()) {
			continue
		}
		archive := filepath.Join(root, entry.Name())
		info, statErr := os.Lstat(archive)
		if statErr != nil || (!info.IsDir() && info.Mode()&os.ModeSymlink == 0) {
			continue
		}
		row := Backup{ID: entry.Name(), Status: "invalid"}
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if m, e := readManifest(archive); e == nil {
				row.CreatedAt, row.MediaCount, row.Bytes = m.CreatedAt, mediaCount(m.Files), totalBytes(m.Files)
				if inventoryMatchesContext(ctx, archive, m) == nil {
					row.Status = "available"
				}
			}
		}
		result.Backups = append(result.Backups, row)
		if len(result.Backups) > MaxBackups {
			result.Truncated = true
			sortBackups(result.Backups)
			result.Backups = result.Backups[:MaxBackups]
		}
	}
	sortBackups(result.Backups)
	return result, nil
}

func sortBackups(backups []Backup) {
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].CreatedAt != backups[j].CreatedAt {
			return backups[i].CreatedAt > backups[j].CreatedAt
		}
		return backups[i].ID > backups[j].ID
	})
}

func (s *Service) Verify(ctx context.Context, id string) (Receipt, error) {
	if !s.verifying.CompareAndSwap(false, true) {
		return Receipt{}, ErrBusy
	}
	defer s.verifying.Store(false)
	if !backupID.MatchString(id) || id == "." || id == ".." {
		return Receipt{}, ErrNotFound
	}
	root, err := s.realRoot()
	if err != nil {
		return Receipt{}, err
	}
	archive := filepath.Join(root, id)
	info, err := os.Lstat(archive)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Receipt{}, ErrNotFound
	}
	m, err := readManifest(archive)
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Receipt{}, ErrTimeout
		}
		return Receipt{}, ErrCanceled
	}
	if err = inventoryMatchesContext(ctx, archive, m); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Receipt{}, ErrTimeout
		}
		if errors.Is(err, context.Canceled) {
			return Receipt{}, ErrCanceled
		}
		return Receipt{}, ErrInvalid
	}
	for _, item := range m.Files {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return Receipt{}, ErrTimeout
			}
			return Receipt{}, ErrCanceled
		}
		size, hash, e := hashFile(ctx, archive, item.Path)
		if e != nil {
			if errors.Is(e, context.DeadlineExceeded) {
				return Receipt{}, ErrTimeout
			}
			if errors.Is(e, context.Canceled) {
				return Receipt{}, ErrCanceled
			}
			return Receipt{}, ErrInvalid
		}
		if size != item.Size || hash != item.SHA256 {
			return Receipt{}, ErrInvalid
		}
	}
	return Receipt{ID: id, VerifiedAt: s.now().UTC().Format(time.RFC3339Nano), MediaCount: mediaCount(m.Files), Bytes: totalBytes(m.Files)}, nil
}

func (s *Service) realRoot() (string, error) {
	if s == nil || s.root == "" {
		return "", ErrUnconfigured
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", ErrUnconfigured
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrUnconfigured
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", ErrUnconfigured
	}
	return resolved, nil
}

func readManifest(dir string) (manifest, error) {
	name := filepath.Join(dir, "manifest.json")
	st, err := os.Lstat(name)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() > MaxManifestBytes {
		return manifest{}, ErrInvalid
	}
	f, err := os.OpenFile(name, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return manifest{}, ErrInvalid
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(bufio.NewReader(f), MaxManifestBytes+1))
	if err != nil || len(data) > MaxManifestBytes || duplicateJSONKeys(data) {
		return manifest{}, ErrInvalid
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var m manifest
	if dec.Decode(&m) != nil {
		return manifest{}, ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return manifest{}, ErrInvalid
	}
	if m.Version != 1 || len(m.Files) == 0 || len(m.Files) > maxManifestFiles {
		return manifest{}, ErrInvalid
	}
	t, err := time.Parse(time.RFC3339Nano, m.CreatedAt)
	if err != nil || t.IsZero() {
		return manifest{}, ErrInvalid
	}
	seen := map[string]bool{}
	hasDump := false
	for _, e := range m.Files {
		if !validRel(e.Path) || seen[e.Path] || (e.Path != "database.dump" && !strings.HasPrefix(e.Path, "media/")) || e.Size < 0 || len(e.SHA256) != 64 {
			return manifest{}, ErrInvalid
		}
		if _, err := hex.DecodeString(e.SHA256); err != nil || strings.ToLower(e.SHA256) != e.SHA256 {
			return manifest{}, ErrInvalid
		}
		seen[e.Path] = true
		if e.Path == "database.dump" {
			hasDump = true
		}
	}
	if !hasDump {
		return manifest{}, ErrInvalid
	}
	return m, nil
}

func duplicateJSONKeys(data []byte) bool {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var value func() error
	value = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return ErrInvalid
				}
				seen[key] = true
				if err := value(); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return ErrInvalid
			}
		case '[':
			for dec.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		return nil
	}
	if value() != nil {
		return true
	}
	_, err := dec.Token()
	return err != io.EOF
}

func validRel(rel string) bool {
	if rel == "" || strings.Contains(rel, "\\") || strings.HasPrefix(rel, "/") || path.Clean(rel) != rel {
		return false
	}
	for _, p := range strings.Split(rel, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}

func inventoryMatches(dir string, m manifest) error {
	return inventoryMatchesContext(context.Background(), dir, m)
}

func inventoryMatchesContext(ctx context.Context, dir string, m manifest) error {
	want := map[string]bool{"manifest.json": true}
	for _, e := range m.Files {
		want[e.Path] = true
	}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || !want[rel] {
			return ErrInvalid
		}
		delete(want, rel)
		return nil
	})
	if err != nil || len(want) != 0 {
		return ErrInvalid
	}
	return nil
}

func hashFile(ctx context.Context, root, rel string) (int64, string, error) {
	if !validRel(rel) {
		return 0, "", ErrInvalid
	}
	p := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		p = filepath.Join(p, part)
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return 0, "", ErrInvalid
		}
		if i < len(parts)-1 && !st.IsDir() {
			return 0, "", ErrInvalid
		}
	}
	f, e := os.OpenFile(p, os.O_RDONLY|openNoFollow, 0)
	if e != nil {
		return 0, "", ErrInvalid
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || !before.Mode().IsRegular() {
		return 0, "", ErrInvalid
	}
	h := sha256.New()
	buf := make([]byte, hashChunk)
	var size int64
	for {
		if e := ctx.Err(); e != nil {
			return 0, "", e
		}
		n, re := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
			size += int64(n)
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return 0, "", re
		}
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || size != after.Size() {
		return 0, "", ErrInvalid
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}
func mediaCount(files []fileEntry) int {
	n := 0
	for _, f := range files {
		if strings.HasPrefix(f.Path, "media/") {
			n++
		}
	}
	return n
}
func totalBytes(files []fileEntry) int64 {
	var n int64
	for _, f := range files {
		if f.Size > 0 && n > int64(^uint64(0)>>1)-f.Size {
			return int64(^uint64(0) >> 1)
		}
		n += f.Size
	}
	return n
}
