package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const replicaChunk = 1 << 20

var (
	ErrReplicaUnconfigured = errors.New("backup replica root is not configured")
	ErrReplicaRoot         = errors.New("backup replica root is invalid or overlaps protected storage")
	ErrReplicaExists       = errors.New("backup already exists in replica root")
	ErrReplicaInvalid      = errors.New("backup could not be replicated safely")
)

// ReplicaReceipt contains only the verified backup identity and its inventory
// summary. It deliberately omits local filesystem paths.
type ReplicaReceipt struct {
	ID           string `json:"id"`
	ReplicatedAt string `json:"replicated_at"`
	MediaCount   int    `json:"media_count"`
	Bytes        int64  `json:"bytes"`
}

// Replicate verifies a published primary backup, copies it to the configured
// existing local replica root, verifies the copied manifest and inventory, and
// only then publishes it under the same ID. It never removes the primary.
// KURA_BACKUP_REPLICA_ROOT must identify a second local directory outside both
// KURA_BACKUP_ROOT and MEDIA_ROOT. This is local replication, not offsite backup.
func (s *Service) Replicate(ctx context.Context, id string) (ReplicaReceipt, error) {
	if !backupID.MatchString(id) || id == "." || id == ".." {
		return ReplicaReceipt{}, ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return ReplicaReceipt{}, replicaContextError(err)
	}
	if s == nil {
		return ReplicaReceipt{}, ErrUnconfigured
	}
	primary, err := s.realRoot()
	if err != nil {
		return ReplicaReceipt{}, err
	}
	replicaSetting := strings.TrimSpace(os.Getenv("KURA_BACKUP_REPLICA_ROOT"))
	if replicaSetting == "" {
		return ReplicaReceipt{}, ErrReplicaUnconfigured
	}
	replica, err := existingRealDirectory(replicaSetting)
	if err != nil {
		return ReplicaReceipt{}, ErrReplicaRoot
	}
	mediaSetting := strings.TrimSpace(os.Getenv("MEDIA_ROOT"))
	if rootsOverlap(primary, replica) {
		return ReplicaReceipt{}, ErrReplicaRoot
	}
	if mediaSetting != "" {
		media, pathErr := canonicalConfiguredPath(mediaSetting)
		if pathErr != nil || rootsOverlap(media, replica) {
			return ReplicaReceipt{}, ErrReplicaRoot
		}
	}

	// Verify is the caller-visible proof gate. Re-read and validate the source
	// manifest after that proof, then check every copied byte while staging.
	verified, err := s.Verify(ctx, id)
	if err != nil {
		return ReplicaReceipt{}, err
	}
	source := filepath.Join(primary, id)
	m, err := validatePublishedDirectory(ctx, source)
	if err != nil {
		return ReplicaReceipt{}, replicaCopyError(ctx, err)
	}
	if _, err := os.Lstat(filepath.Join(replica, id)); err == nil {
		return ReplicaReceipt{}, ErrReplicaExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return ReplicaReceipt{}, ErrReplicaRoot
	}

	stage, err := os.MkdirTemp(replica, ".kura-replica-"+id+"-")
	if err != nil {
		return ReplicaReceipt{}, replicaCopyError(ctx, err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := copyVerifiedBackup(ctx, source, stage, m); err != nil {
		return ReplicaReceipt{}, replicaCopyError(ctx, err)
	}
	if err := verifyReplicaStage(ctx, stage, m); err != nil {
		return ReplicaReceipt{}, replicaCopyError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return ReplicaReceipt{}, replicaContextError(err)
	}
	if err := publishReplicaNoReplace(replica, stage, id); err != nil {
		if errors.Is(err, os.ErrExist) || errors.Is(err, ErrReplicaExists) {
			return ReplicaReceipt{}, ErrReplicaExists
		}
		return ReplicaReceipt{}, replicaCopyError(ctx, err)
	}
	published = true
	return ReplicaReceipt{
		ID: id, ReplicatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		MediaCount: verified.MediaCount, Bytes: verified.Bytes,
	}, nil
}

func copyVerifiedBackup(ctx context.Context, source, stage string, m manifest) error {
	for _, item := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := copyManifestFile(ctx, source, stage, item); err != nil {
			return err
		}
	}
	manifestInfo, err := openRegularNoFollow(source, "manifest.json")
	if err != nil {
		return err
	}
	defer manifestInfo.Close()
	manifestStat, err := manifestInfo.Stat()
	if err != nil || manifestStat.Size() > MaxManifestBytes {
		return ErrInvalid
	}
	manifestTarget := filepath.Join(stage, "manifest.json")
	out, err := os.OpenFile(manifestTarget, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	var copied int64
	if copied, err = copyBounded(ctx, out, manifestInfo, manifestStat.Size()); err == nil && copied != manifestStat.Size() {
		err = ErrInvalid
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func copyManifestFile(ctx context.Context, source, stage string, item fileEntry) error {
	in, err := openRegularNoFollow(source, item.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	if item.Path != "database.dump" {
		parent := filepath.Dir(filepath.Join(stage, filepath.FromSlash(item.Path)))
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
	}
	target := filepath.Join(stage, filepath.FromSlash(item.Path))
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := copyBoundedHash(ctx, out, in, h, item.Size)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != item.Size || hex.EncodeToString(h.Sum(nil)) != item.SHA256 {
		return ErrInvalid
	}
	return nil
}

func verifyReplicaStage(ctx context.Context, stage string, expected manifest) error {
	actual, err := validatePublishedDirectory(ctx, stage)
	if err != nil {
		return err
	}
	if actual.Version != expected.Version || actual.CreatedAt != expected.CreatedAt || len(actual.Files) != len(expected.Files) {
		return ErrInvalid
	}
	for i, item := range actual.Files {
		if item != expected.Files[i] {
			return ErrInvalid
		}
	}
	for _, item := range actual.Files {
		size, hash, err := hashFile(ctx, stage, item.Path)
		if err != nil {
			return err
		}
		if size != item.Size || hash != item.SHA256 {
			return ErrInvalid
		}
	}
	return nil
}

func openRegularNoFollow(root, rel string) (*os.File, error) {
	if !validRel(rel) {
		return nil, ErrInvalid
	}
	current, err := os.OpenRoot(root)
	if err != nil {
		return nil, ErrInvalid
	}
	defer func() { _ = current.Close() }()
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		before, statErr := current.Lstat(part)
		if statErr != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalid
		}
		next, openErr := current.OpenRoot(part)
		if openErr != nil {
			return nil, ErrInvalid
		}
		after, statErr := next.Stat(".")
		if statErr != nil || !os.SameFile(before, after) {
			_ = next.Close()
			return nil, ErrInvalid
		}
		_ = current.Close()
		current = next
	}
	name := parts[len(parts)-1]
	before, err := current.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalid
	}
	f, err := current.OpenFile(name, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !os.SameFile(before, st) {
		_ = f.Close()
		return nil, ErrInvalid
	}
	return f, nil
}

func copyBounded(ctx context.Context, dst io.Writer, src io.Reader, expectedSize int64) (int64, error) {
	return copyBoundedHash(ctx, dst, src, nil, expectedSize)
}

func copyBoundedHash(ctx context.Context, dst io.Writer, src io.Reader, hash io.Writer, expectedSize int64) (int64, error) {
	if expectedSize < 0 {
		return 0, ErrInvalid
	}
	buf := make([]byte, replicaChunk)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if int64(n) > expectedSize-copied {
				return copied, ErrInvalid
			}
			chunk := buf[:n]
			written, writeErr := dst.Write(chunk)
			if hash != nil && written > 0 {
				_, _ = hash.Write(chunk[:written])
			}
			copied += int64(written)
			if writeErr != nil {
				return copied, writeErr
			}
			if written != n {
				return copied, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			if copied != expectedSize {
				return copied, ErrInvalid
			}
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
	}
}

func existingRealDirectory(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrUnconfigured
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrUnconfigured
	}
	return filepath.EvalSymlinks(abs)
}

func canonicalConfiguredPath(raw string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	// Resolve the nearest existing ancestor so a not-yet-created MEDIA_ROOT
	// cannot hide an overlap through a symlinked parent directory.
	ancestor := abs
	var suffix []string
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", ErrReplicaRoot
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return filepath.Clean(resolved), nil
}

func rootsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return withinPath(a, b) || withinPath(b, a)
}

func withinPath(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func replicaContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	return ErrCanceled
}

func replicaCopyError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return replicaContextError(ctxErr)
	}
	if errors.Is(err, os.ErrExist) {
		return ErrReplicaExists
	}
	return ErrReplicaInvalid
}

// publishReplicaNoReplace performs a fast existence check and then delegates
// to the platform's atomic no-replace directory rename.
func publishReplicaNoReplace(root, stage, id string) error {
	destination := filepath.Join(root, id)
	if _, err := os.Lstat(destination); err == nil {
		return ErrReplicaExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return replicaRenameNoReplace(stage, destination)
}
