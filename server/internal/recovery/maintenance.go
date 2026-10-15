package recovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	minimumBackupInterval = time.Hour
	maintenanceTimeout    = 20 * time.Minute
	backupTimeLayout      = "backup-20060102T150405Z-"
	maxMaintenanceEntries = 10000
)

var ErrNotDue = errors.New("scheduled backup is not due")

var serviceBackupName = regexp.MustCompile(`^backup-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// MaintenanceConfig controls periodic local backups and their retention. An
// unset interval disables both scheduling and retention.
type MaintenanceConfig struct {
	Interval time.Duration
	Keep     int
	Enabled  bool
}

// ParseMaintenanceConfig validates the two operator-facing environment values.
// Retention is enabled only with scheduling; an unset keep value then defaults
// to seven verified backups.
func ParseMaintenanceConfig(interval, keep string) (MaintenanceConfig, error) {
	interval = strings.TrimSpace(interval)
	if interval == "" {
		return MaintenanceConfig{}, nil
	}
	d, err := time.ParseDuration(interval)
	if err != nil || d < minimumBackupInterval {
		return MaintenanceConfig{}, fmt.Errorf("KURA_BACKUP_INTERVAL must be at least 1h")
	}
	result := MaintenanceConfig{Interval: d, Keep: 7, Enabled: true}
	keep = strings.TrimSpace(keep)
	if keep != "" {
		n, err := strconv.Atoi(keep)
		if err != nil || n < 1 {
			return MaintenanceConfig{}, fmt.Errorf("KURA_BACKUP_KEEP must be a positive integer")
		}
		result.Keep = n
	}
	return result, nil
}

func MaintenanceConfigFromEnv() (MaintenanceConfig, error) {
	return ParseMaintenanceConfig(os.Getenv("KURA_BACKUP_INTERVAL"), os.Getenv("KURA_BACKUP_KEEP"))
}

// StartMaintenance starts the opt-in periodic backup loop. The first attempt
// waits one full interval, so a process restart cannot immediately repeat a
// recently completed schedule. The shared advisory lock still arbitrates
// processes when their timers coincide.
func (s *Service) StartMaintenance(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return ErrShuttingDown
	}
	if s.lifeCancel != nil {
		s.lifeCancel()
	}
	s.lifecycle, s.lifeCancel = context.WithCancel(ctx)
	lifecycle := s.lifecycle
	s.mu.Unlock()
	config, err := MaintenanceConfigFromEnv()
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return ErrShuttingDown
	}
	s.maintenance = config
	s.mu.Unlock()
	if !config.Enabled {
		return nil
	}
	go s.maintenanceLoop(lifecycle, config)
	return nil
}

func (s *Service) maintenanceLoop(ctx context.Context, config MaintenanceConfig) {
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			attemptCtx, cancel := context.WithTimeout(ctx, maintenanceTimeout)
			_, err := s.RunMaintenance(attemptCtx)
			cancel()
			if err != nil && !errors.Is(err, ErrNotDue) && !errors.Is(err, ErrActive) && !errors.Is(err, context.Canceled) {
				// Keep the loop alive; each failure is bounded and retried at the
				// next interval. The job record remains the source of backup status.
				fmt.Fprintln(os.Stderr, "scheduled backup attempt failed; inspect the newest backup job")
			}
		}
	}
}

// RunMaintenance performs one scheduler check. It returns ErrNotDue when the
// newest recognized backup job or published directory is younger than the
// configured interval. Start persists a running job while holding the global
// advisory lock, so concurrent processes cannot launch overlapping backups.
func (s *Service) RunMaintenance(ctx context.Context) (Job, error) {
	if s == nil {
		return Job{}, ErrUnconfigured
	}
	s.mu.Lock()
	config := s.maintenance
	s.mu.Unlock()
	if !config.Enabled {
		return Job{}, nil
	}
	guardCtx, cancel := context.WithTimeout(ctx, maintenanceTimeout)
	defer cancel()
	return s.start(guardCtx, func(root string) error {
		latest, err := latestBackupTime(guardCtx, root)
		if err != nil {
			return err
		}
		if !latest.IsZero() && s.clock().Sub(latest) < config.Interval {
			return ErrNotDue
		}
		return guardCtx.Err()
	})
}

func latestBackupTime(ctx context.Context, root string) (time.Time, error) {
	latest := time.Time{}
	consider := func(name string) {
		t, ok := backupTimeFromID(name)
		if ok && t.After(latest) {
			latest = t
		}
	}
	entries, err := readMaintenanceEntries(ctx, root)
	if err != nil {
		return time.Time{}, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
		consider(entry.Name())
	}
	jobs, err := checkedJobDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	jobEntries, err := readMaintenanceEntries(ctx, jobs)
	if err != nil {
		return time.Time{}, err
	}
	for _, entry := range jobEntries {
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			consider(strings.TrimSuffix(entry.Name(), ".json"))
		}
	}
	return latest, nil
}

func backupTimeFromID(name string) (time.Time, bool) {
	if !serviceBackupName.MatchString(name) {
		return time.Time{}, false
	}
	t, err := time.Parse(backupTimeLayout, name[:len(backupTimeLayout)])
	if err == nil {
		return t, true
	}
	return time.Time{}, false
}

func readMaintenanceEntries(ctx context.Context, dir string) ([]os.DirEntry, error) {
	file, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(maxMaintenanceEntries + 1)
	if len(entries) > maxMaintenanceEntries {
		return nil, fmt.Errorf("maintenance directory exceeds %d entries", maxMaintenanceEntries)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

type verifiedBackup struct {
	id        string
	createdAt time.Time
	info      os.FileInfo
}

func (s *Service) pruneLocked(ctx context.Context, root string, keep int, current string) error {
	if keep < 1 || !serviceBackupName.MatchString(current) {
		return ErrInvalid
	}
	currentJob, err := s.readJob(root, current)
	if err != nil || (currentJob.Status != statusSucceeded && currentJob.Status != statusRunning) {
		return ErrInvalid
	}
	if err := s.verifyPrimaryAndReplica(ctx, root, current); err != nil {
		return fmt.Errorf("verify newest replicated backup before retention: %w", err)
	}
	entries, err := readMaintenanceEntries(ctx, root)
	if err != nil {
		return err
	}
	verified := make([]verifiedBackup, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := entry.Name()
		if !serviceBackupName.MatchString(id) {
			continue
		}
		path := filepath.Join(root, id)
		before, err := os.Lstat(path)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			continue
		}
		job, err := s.readJob(root, id)
		if err != nil || (job.Status != statusSucceeded && !(id == current && job.Status == statusRunning)) {
			continue
		}
		m, err := validatePublishedDirectory(ctx, path)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			continue
		}
		if err := verifyManifestFiles(ctx, path, m); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			continue
		}
		if err := s.verifyPrimaryAndReplica(ctx, root, id); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			continue
		}
		verified = append(verified, verifiedBackup{id: id, createdAt: mustParseManifestTime(m.CreatedAt), info: before})
	}
	sort.Slice(verified, func(i, j int) bool {
		if !verified[i].createdAt.Equal(verified[j].createdAt) {
			return verified[i].createdAt.After(verified[j].createdAt)
		}
		return verified[i].id > verified[j].id
	})
	if len(verified) <= keep {
		return nil
	}
	protected := map[string]bool{current: true}
	for i := 0; i < keep && i < len(verified); i++ {
		protected[verified[i].id] = true
	}
	for _, candidate := range verified {
		if protected[candidate.id] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(root, candidate.id)
		currentInfo, err := os.Lstat(path)
		if err != nil || !currentInfo.IsDir() || currentInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(candidate.info, currentInfo) {
			continue
		}
		job, err := s.readJob(root, candidate.id)
		if err != nil || job.Status != statusSucceeded {
			continue
		}
		if err := retireVerifiedBackup(ctx, root, candidate.id, candidate.info, func(checkCtx context.Context, quarantinePath string) error {
			return s.verifyBackupAndReplica(checkCtx, quarantinePath, candidate.id)
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) verifyPrimaryAndReplica(ctx context.Context, primaryRoot, id string) error {
	return s.verifyBackupAndReplica(ctx, filepath.Join(primaryRoot, id), id)
}

func (s *Service) verifyBackupAndReplica(ctx context.Context, primaryPath, id string) error {
	primaryManifest, err := readManifest(primaryPath)
	if err != nil {
		return err
	}
	if err := validatePublishedBackup(ctx, primaryPath); err != nil {
		return err
	}
	replicaRoot := strings.TrimSpace(os.Getenv("KURA_BACKUP_REPLICA_ROOT"))
	if replicaRoot == "" {
		return ErrReplicaUnconfigured
	}
	replica := New(replicaRoot)
	replicaPath := filepath.Join(replicaRoot, id)
	replicaManifest, err := readManifest(replicaPath)
	if err != nil {
		return err
	}
	if _, err := replica.Verify(ctx, id); err != nil {
		return err
	}
	if !sameManifest(primaryManifest, replicaManifest) {
		return ErrInvalid
	}
	return nil
}

// retireVerifiedBackup atomically moves a candidate out of its public backup
// name before the final inode and content checks. The optional hook is used by
// the race regression test to replace the path in the narrow rename window.
func retireVerifiedBackup(ctx context.Context, root, id string, expected os.FileInfo, verify func(context.Context, string) error, beforeRename func() error) error {
	path := filepath.Join(root, id)
	initial, err := os.Lstat(path)
	if err != nil || !initial.IsDir() || initial.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, initial) {
		return errors.New("retention candidate changed before retirement")
	}
	quarantine, err := newRetirementPath(root, id)
	if err != nil {
		return errors.New("could not prepare backup retirement")
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return errors.New("could not complete backup retirement")
		}
	}
	if err := replicaRenameNoReplace(path, quarantine); err != nil {
		return errors.New("could not move backup into retirement quarantine")
	}
	restore := func() error {
		if err := replicaRenameNoReplace(quarantine, path); err != nil {
			return errors.New("backup remains in retirement quarantine")
		}
		return nil
	}
	moved, err := os.Lstat(quarantine)
	if err != nil || !moved.IsDir() || moved.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, moved) {
		if restoreErr := restore(); restoreErr != nil {
			return restoreErr
		}
		return errors.New("retention candidate changed during retirement")
	}
	if err := ctx.Err(); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return restoreErr
		}
		return err
	}
	if err := verify(ctx, quarantine); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return restoreErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errors.New("retention candidate failed quarantine verification")
	}
	final, err := os.Lstat(quarantine)
	if err != nil || !final.IsDir() || final.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, final) {
		if restoreErr := restore(); restoreErr != nil {
			return restoreErr
		}
		return errors.New("retention candidate changed after quarantine verification")
	}
	if err := ctx.Err(); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return restoreErr
		}
		return err
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return errors.New("could not remove verified retired backup")
	}
	return nil
}

func newRetirementPath(root, id string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	// This prefix is intentionally distinct from sweepStaging's prefixes, so
	// an interrupted retirement always leaves its data recoverable.
	return filepath.Join(root, ".kura-retired-"+hex.EncodeToString(nonce[:])+"-"+id), nil
}

func sameManifest(a, b manifest) bool {
	if a.Version != b.Version || a.CreatedAt != b.CreatedAt || len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Files {
		if a.Files[i] != b.Files[i] {
			return false
		}
	}
	return true
}

func verifyManifestFiles(ctx context.Context, dir string, m manifest) error {
	for _, item := range m.Files {
		size, hash, err := hashFile(ctx, dir, item.Path)
		if err != nil {
			return err
		}
		if size != item.Size || hash != item.SHA256 {
			return ErrInvalid
		}
	}
	return nil
}

func mustParseManifestTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}
