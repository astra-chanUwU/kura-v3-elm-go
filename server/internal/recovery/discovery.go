package recovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const maxJobHistory = 4096

var ErrJobHistoryTooLarge = errors.New("backup job history exceeds discovery limit")

// discoverActiveJob finds the newest persisted running job. A bounded history
// is important because these records live beside the backup root indefinitely.
// If the bound is exceeded, discovery fails closed instead of claiming that no
// backup is running when an unexamined record might be active.
func (s *Service) discoverActiveJob(ctx context.Context, root string) (*Job, error) {
	dir, err := checkedJobDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check backup job directory: %w", err)
	}

	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalid
	}
	f, err := os.OpenFile(dir, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return nil, ErrInvalid
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.IsDir() || !os.SameFile(info, opened) {
		return nil, ErrInvalid
	}

	entries, err := f.ReadDir(maxJobHistory + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read backup job directory: %w", err)
	}
	if len(entries) > maxJobHistory {
		return nil, ErrJobHistoryTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if backupID.MatchString(id) && id != "." && id != ".." {
			ids = append(ids, id)
		}
	}
	// Backup IDs begin with a UTC timestamp, so lexical order is chronological.
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		job, err := s.readJobRetry(root, id)
		if err != nil {
			return nil, fmt.Errorf("read backup job record: %w", err)
		}
		if job.Status == statusRunning {
			return &job, nil
		}
	}
	return nil, nil
}

// Atomic state-file replacement can race the readJob Lstat/open identity check.
// Retry that bounded read once so an ordinary status update does not fail list.
func (s *Service) readJobRetry(root, id string) (Job, error) {
	job, err := s.readJob(root, id)
	if errors.Is(err, ErrInvalid) {
		return s.readJob(root, id)
	}
	return job, err
}
