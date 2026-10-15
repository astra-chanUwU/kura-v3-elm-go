package recovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	createTimeout   = 30 * time.Minute
	jobDirName      = ".kura-backup-jobs"
	statusRunning   = "running"
	statusSucceeded = "succeeded"
	statusFailed    = "failed"
	statusCanceled  = "canceled"
	backupLockClass = 87203461
	backupLockObj   = 102341
)

var remoteSettingKeys = []string{
	"S3_ENDPOINT", "AWS_S3_ENDPOINT", "S3_BUCKET", "AWS_S3_BUCKET",
	"S3_URL_PREFIX", "MEDIA_URL_PREFIX", "S3_PUBLIC_URL_PREFIX",
}

// Job is the server-owned state of one backup request. Succeeded means the
// directory was published after the proved snapshot copy verified its manifest.
// Callers must not treat running, failed, or canceled as a completed backup.
type Job struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type backupRunner func(ctx context.Context, env []string, mediaRoot, output string) error
type lockHolder func(ctx context.Context, env []string) (release func(), err error)

// SetTestBackend replaces the snapshot runner and the cross-process lock.
// Production leaves both unset.
func (s *Service) SetTestBackend(run backupRunner, lock lockHolder) {
	if s == nil {
		return
	}
	s.run = run
	s.lock = lock
}

// Start begins one local backup. The fixed destination is KURA_BACKUP_ROOT.
// A second request while one is active returns ErrActive.
func (s *Service) Start(ctx context.Context) (Job, error) {
	if remoteConfigured() {
		return Job{}, ErrUnsupported
	}
	root, err := s.realRoot()
	if err != nil {
		return Job{}, err
	}
	media := strings.TrimSpace(os.Getenv("MEDIA_ROOT"))
	if media == "" {
		return Job{}, ErrUnconfigured
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	env, err := pgProcessEnv(databaseURL)
	if err != nil {
		return Job{}, ErrUnconfigured
	}
	if !s.creating.CompareAndSwap(false, true) {
		return Job{}, ErrActive
	}
	release, err := s.locker()(ctx, env)
	if err != nil {
		s.creating.Store(false)
		return Job{}, err
	}
	id := newBackupID(s.clock())
	job := Job{ID: id, Status: statusRunning}
	if err := s.writeJob(root, job); err != nil {
		release()
		s.creating.Store(false)
		return Job{}, err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), createTimeout)
	s.mu.Lock()
	s.active = id
	s.cancel = cancel
	s.mu.Unlock()
	go s.finish(runCtx, cancel, release, root, env, media, filepath.Join(root, id), id)
	return job, nil
}

// Job returns the persisted request state. It does not treat a directory name
// as success when no job record exists.
func (s *Service) Job(id string) (Job, error) {
	if !backupID.MatchString(id) {
		return Job{}, ErrNotFound
	}
	root, err := s.realRoot()
	if err != nil {
		return Job{}, err
	}
	return s.readJob(root, id)
}

// Cancel asks the active attempt to stop. A published directory is left in
// place; an unfinished staging directory is removed when the attempt exits.
func (s *Service) Cancel(id string) (Job, error) {
	job, err := s.Job(id)
	if err != nil {
		return Job{}, err
	}
	if job.Status != statusRunning {
		return job, nil
	}
	root, err := s.realRoot()
	if err != nil {
		return Job{}, err
	}
	if err := os.WriteFile(s.cancelPath(root, id), []byte("1\n"), 0o600); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	if s.active == id && s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	return s.Job(id)
}

// Reconcile marks jobs left running by a dead process as failed and removes
// unpublished staging directories. A directory that already passed publication
// is recorded as succeeded.
func (s *Service) Reconcile() error {
	root, err := s.realRoot()
	if errors.Is(err, ErrUnconfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.creating.Load() {
		return nil
	}
	entries, err := os.ReadDir(s.jobDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			s.sweepStaging(root)
			return nil
		}
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		job, err := s.readJob(root, id)
		if err != nil || job.Status != statusRunning {
			continue
		}
		if info, statErr := os.Lstat(filepath.Join(root, id)); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			job.Status = statusSucceeded
			job.Error = ""
		} else {
			job.Status = statusFailed
			job.Error = "backup was interrupted before publication"
		}
		if err := s.writeJob(root, job); err != nil {
			return err
		}
	}
	s.sweepStaging(root)
	return nil
}

func (s *Service) finish(ctx context.Context, cancel context.CancelFunc, release func(), root string, env []string, media, output, id string) {
	defer s.creating.Store(false)
	defer release()
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go s.watchCancel(ctx, done, root, id, cancel)
	err := s.runner()(ctx, env, media, output)
	s.sweepStaging(root)
	s.mu.Lock()
	if s.active == id {
		s.active = ""
		s.cancel = nil
	}
	s.mu.Unlock()
	_ = os.Remove(s.cancelPath(root, id))
	job := Job{ID: id, Status: statusSucceeded}
	published := false
	if info, statErr := os.Lstat(output); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		published = true
	}
	switch {
	case published:
		job.Status = statusSucceeded
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		job.Status = statusFailed
		job.Error = "backup timed out"
	case errors.Is(ctx.Err(), context.Canceled):
		job.Status = statusCanceled
	case err != nil:
		job.Status = statusFailed
		job.Error = safeBackupError(err)
	}
	_ = s.writeJob(root, job)
}

func (s *Service) watchCancel(ctx context.Context, done <-chan struct{}, root, id string, cancel context.CancelFunc) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if _, err := os.Lstat(s.cancelPath(root, id)); err == nil {
				cancel()
				return
			}
		}
	}
}

func (s *Service) runner() backupRunner {
	if s.run != nil {
		return s.run
	}
	return defaultBackupRun
}

func (s *Service) locker() lockHolder {
	if s.lock != nil {
		return s.lock
	}
	return holdBackupLock
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func remoteConfigured() bool {
	for _, key := range remoteSettingKeys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func newBackupID(now time.Time) string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return now.UTC().Format("backup-20060102T150405Z-") + hex.EncodeToString(buf[:])
}

func (s *Service) jobDir(root string) string {
	return filepath.Join(root, jobDirName)
}

func (s *Service) jobPath(root, id string) string {
	return filepath.Join(s.jobDir(root), id+".json")
}

func (s *Service) cancelPath(root, id string) string {
	return filepath.Join(s.jobDir(root), id+".cancel")
}

func (s *Service) writeJob(root string, job Job) error {
	dir := s.jobDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(s.jobPath(root, job.ID), body, 0o600)
}

func (s *Service) readJob(root, id string) (Job, error) {
	body, err := os.ReadFile(s.jobPath(root, id))
	if err != nil {
		if os.IsNotExist(err) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(body, &job); err != nil || job.ID != id {
		return Job{}, ErrInvalid
	}
	return job, nil
}

func (s *Service) sweepStaging(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == jobDirName {
			continue
		}
		if strings.HasPrefix(name, ".kura-backup-") || strings.HasPrefix(name, ".kura-proof-snap-") {
			_ = os.RemoveAll(filepath.Join(root, name))
		}
	}
}

func pgProcessEnv(databaseURL string) ([]string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return nil, ErrUnconfigured
	}
	database := strings.TrimPrefix(parsed.Path, "/")
	if database == "" || strings.Contains(database, "/") {
		return nil, ErrUnconfigured
	}
	drop := map[string]bool{
		"DATABASE_URL": true, "KURA_BACKUP_TEST_ADMIN_URL": true,
		"PGPASSWORD": true, "PGHOST": true, "PGPORT": true,
		"PGUSER": true, "PGDATABASE": true, "PGSSLMODE": true,
		"KURA_ONLINE_BACKUP_EXTERNAL_LOCK": true,
	}
	for _, key := range remoteSettingKeys {
		drop[key] = true
	}
	env := make([]string, 0, 32)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[strings.ToUpper(name)] || strings.Contains(entry, "://") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PGDATABASE="+database, "KURA_ONLINE_BACKUP_EXTERNAL_LOCK=1")
	if host := parsed.Hostname(); host != "" {
		env = append(env, "PGHOST="+host)
	}
	if port := parsed.Port(); port != "" {
		env = append(env, "PGPORT="+port)
	}
	if parsed.User != nil {
		if name := parsed.User.Username(); name != "" {
			env = append(env, "PGUSER="+name)
		}
		if password, ok := parsed.User.Password(); ok {
			env = append(env, "PGPASSWORD="+password)
		}
	}
	if mode := parsed.Query().Get("sslmode"); mode != "" {
		env = append(env, "PGSSLMODE="+mode)
	}
	return env, nil
}

func holdBackupLock(ctx context.Context, env []string) (func(), error) {
	dir, err := os.MkdirTemp("", "kura-backup-lock-")
	if err != nil {
		return nil, err
	}
	marker := filepath.Join(dir, "lock.txt")
	quoted := strings.ReplaceAll(filepath.ToSlash(marker), "'", "''")
	// Stay in psql's command loop. pg_sleep would keep the session and the
	// advisory lock after the client exits, because that wait does not watch
	// the client socket.
	body := strings.Join([]string{
		"\\pset tuples_only on",
		"\\pset format unaligned",
		"\\pset pager off",
		"\\o '" + quoted + "'",
		fmt.Sprintf("SELECT CASE WHEN pg_try_advisory_lock(%d, %d) THEN '1' ELSE '0' END;", backupLockClass, backupLockObj),
		"\\o",
		"",
	}, "\n")
	psql, err := exec.LookPath("psql")
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("required tool is missing")
	}
	cmd := exec.Command(psql, "-X", "-v", "ON_ERROR_STOP=1")
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("required tool is missing")
	}
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	if _, err := io.WriteString(stdin, body); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("could not lock backup creation")
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_ = stdin.Close()
			select {
			case <-waited:
			case <-time.After(2 * time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				select {
				case <-waited:
				case <-time.After(2 * time.Second):
				}
			}
			_ = os.RemoveAll(dir)
		})
	}
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) && text == "" {
		if data, readErr := os.ReadFile(marker); readErr == nil {
			text = strings.TrimSpace(string(data))
		}
		if text != "" {
			break
		}
		select {
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		case <-waited:
			deadline = time.Now()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if text != "1" {
		release()
		if text == "0" {
			return nil, ErrActive
		}
		return nil, fmt.Errorf("could not lock backup creation")
	}
	return release, nil
}

const pythonLauncher = `
import importlib.util, os, sys
path = sys.argv[1]
spec = importlib.util.spec_from_file_location("kura_online_backup_proof", path)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
external = os.environ.get("KURA_ONLINE_BACKUP_EXTERNAL_LOCK") == "1"
mod.run_online_backup(sys.argv[2], sys.argv[3], mod.open_postgres_snapshot(os.environ, external_lock=external))
`

func defaultBackupRun(ctx context.Context, env []string, mediaRoot, output string) error {
	script := strings.TrimSpace(os.Getenv("KURA_ONLINE_BACKUP_SCRIPT"))
	if script == "" {
		script = filepath.Join("scripts", "online_backup_proof.py")
	}
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("backup tool is unavailable")
	}
	python, err := findPython()
	if err != nil {
		return fmt.Errorf("backup tool is unavailable")
	}
	cmd := exec.CommandContext(ctx, python, "-c", pythonLauncher, script, mediaRoot, output)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup failed: %s", redactTool(stderr.String()))
	}
	return nil
}

func findPython() (string, error) {
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(path), `\windowsapps\`) {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("backup tool is unavailable")
}

func safeBackupError(err error) string {
	text := redactTool(err.Error())
	switch {
	case strings.Contains(text, "atomic publication"):
		return "atomic publication is unavailable on this platform"
	case strings.Contains(text, "missing referenced media"):
		return "referenced media is missing"
	case strings.Contains(text, "does not match"):
		return "referenced media does not match the snapshot"
	case strings.Contains(text, "pg_dump failed"):
		return "database dump failed"
	case strings.Contains(text, "interrupted"):
		return "backup interrupted"
	case strings.Contains(text, "S3"):
		return "S3 backups are not supported"
	case strings.Contains(text, "already running"):
		return "a backup is already running"
	case strings.Contains(text, "disk") || strings.Contains(text, "no space"):
		return "backup storage is full"
	default:
		return "backup failed"
	}
}

func redactTool(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(line, "://") || strings.Contains(lower, "password") || strings.Contains(lower, "pgpassword") || strings.Contains(lower, "connection to server") {
			kept = append(kept, "[redacted]")
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if len(out) > 500 {
		return out[len(out)-500:]
	}
	return out
}
