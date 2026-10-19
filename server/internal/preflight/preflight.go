// Package preflight checks operator configuration before startup.
// Reports name the failed check and never include credentials or database URLs.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
)

const toolMajor = 16

// Check is one operator-visible result. Detail is a fixed phrase.
type Check struct {
	Name   string
	Status string
	Detail string
}

// Report is the full preflight result. OK is false when any required check failed.
type Report struct {
	OK     bool
	Checks []Check
}

// Probe supplies the environment and side effects. Tests replace the functions
// that would touch a database, the process PATH, or a filesystem.
type Probe struct {
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	CommandOut func(context.Context, string, ...string) (string, error)
	Database   func(context.Context, string) (bool, bool, error)
	Writable   func(string) error
}

// Run writes a line-oriented report and returns 0 only when every required check passed.
func Run(w io.Writer, probe Probe) int {
	report := Evaluate(context.Background(), probe)
	for _, check := range report.Checks {
		fmt.Fprintf(w, "%s\t%s\t%s\n", check.Status, check.Name, check.Detail)
	}
	if report.OK {
		return 0
	}
	return 1
}

// Evaluate collects configuration, connectivity, storage, and tool checks.
func Evaluate(ctx context.Context, probe Probe) Report {
	if probe.Getenv == nil {
		probe.Getenv = os.Getenv
	}
	if probe.LookPath == nil {
		probe.LookPath = exec.LookPath
	}
	if probe.CommandOut == nil {
		probe.CommandOut = commandOutput
	}
	if probe.Database == nil {
		probe.Database = queryMigrationState
	}
	if probe.Writable == nil {
		probe.Writable = writableDirectory
	}
	checks := make([]Check, 0, 8)
	databaseURL := strings.TrimSpace(probe.Getenv("DATABASE_URL"))
	if databaseURL == "" || strings.ContainsAny(databaseURL, "\r\n") {
		checks = append(checks, Check{Name: "database", Status: "fail", Detail: "DATABASE_URL is not set"})
	} else {
		checks = append(checks, Check{Name: "database", Status: "ok", Detail: "configured"})
		posts, jobs, err := probe.Database(ctx, databaseURL)
		if err != nil {
			checks = append(checks, Check{Name: "database_connectivity", Status: "fail", Detail: "database is unavailable"})
			checks = append(checks, Check{Name: "migrations", Status: "fail", Detail: "not checked because the database is unavailable"})
		} else {
			checks = append(checks, Check{Name: "database_connectivity", Status: "ok", Detail: "reachable"})
			if posts && jobs {
				checks = append(checks, Check{Name: "migrations", Status: "ok", Detail: "posts and jobs tables are present"})
			} else {
				checks = append(checks, Check{Name: "migrations", Status: "fail", Detail: "database is reachable and has not been migrated; run scripts/db-migrate.sh without seeding"})
			}
		}
	}
	checks = append(checks, directoryCheck("media_root", probe.Getenv("MEDIA_ROOT"), true, probe.Writable))
	backupRoot := strings.TrimSpace(probe.Getenv("KURA_BACKUP_ROOT"))
	checks = append(checks, directoryCheck("backup_root", backupRoot, false, probe.Writable))
	replica := strings.TrimSpace(probe.Getenv("KURA_BACKUP_REPLICA_ROOT"))
	checks = append(checks, replicaCheck(probe.Getenv("MEDIA_ROOT"), backupRoot, replica, probe.Writable))
	if cfg, err := recovery.ParseMaintenanceConfig(probe.Getenv("KURA_BACKUP_INTERVAL"), probe.Getenv("KURA_BACKUP_KEEP")); err != nil {
		checks = append(checks, Check{Name: "backup_schedule", Status: "fail", Detail: "backup schedule is invalid"})
	} else if cfg.Enabled && backupRoot == "" {
		checks = append(checks, Check{Name: "backup_schedule", Status: "fail", Detail: "backup schedule requires KURA_BACKUP_ROOT"})
	} else if cfg.Enabled {
		checks = append(checks, Check{Name: "backup_schedule", Status: "ok", Detail: "scheduled backups are enabled"})
	} else {
		checks = append(checks, Check{Name: "backup_schedule", Status: "disabled", Detail: "scheduled backups are disabled"})
	}
	checks = append(checks, toolsCheck(ctx, probe))
	checks = append(checks, ffmpegCheck(ctx, probe))
	ok := true
	for _, check := range checks {
		if check.Status == "fail" {
			ok = false
			break
		}
	}
	return Report{OK: ok, Checks: checks}
}

func directoryCheck(name, raw string, required bool, writable func(string) error) Check {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return Check{Name: name, Status: "fail", Detail: name + " is not set"}
		}
		return Check{Name: name, Status: "disabled", Detail: name + " is unset"}
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, "@") || strings.ContainsAny(raw, "\r\n") {
		return Check{Name: name, Status: "fail", Detail: name + " is not a local directory"}
	}
	if err := writable(raw); err != nil {
		return Check{Name: name, Status: "fail", Detail: name + " is not writable"}
	}
	return Check{Name: name, Status: "ok", Detail: "writable"}
}

func replicaCheck(media, backup, replica string, writable func(string) error) Check {
	replica = strings.TrimSpace(replica)
	if replica == "" {
		return Check{Name: "replica_root", Status: "disabled", Detail: "replica root is unset"}
	}
	check := directoryCheck("replica_root", replica, true, writable)
	if check.Status != "ok" {
		return check
	}
	if overlaps(replica, media) || overlaps(replica, backup) {
		return Check{Name: "replica_root", Status: "fail", Detail: "replica root overlaps media or backup storage"}
	}
	return Check{Name: "replica_root", Status: "ok", Detail: "writable"}
}

func overlaps(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "." || b == "." || a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	rel, err := filepath.Rel(a, b)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func toolsCheck(ctx context.Context, probe Probe) Check {
	for _, name := range []string{"psql", "pg_dump", "pg_restore"} {
		path, err := probe.LookPath(name)
		if err != nil || strings.TrimSpace(path) == "" {
			return Check{Name: "backup_tools", Status: "fail", Detail: "psql, pg_dump, and pg_restore are required"}
		}
		if name == "pg_dump" {
			out, err := probe.CommandOut(ctx, path, "--version")
			if err != nil || majorVersion(out) < toolMajor {
				return Check{Name: "backup_tools", Status: "fail", Detail: "pg_dump must be PostgreSQL 16 or newer"}
			}
		}
	}
	return Check{Name: "backup_tools", Status: "ok", Detail: "psql, pg_dump, and pg_restore are available"}
}

func ffmpegCheck(ctx context.Context, probe Probe) Check {
	path, err := probe.LookPath("ffmpeg")
	if err != nil || strings.TrimSpace(path) == "" {
		return Check{Name: "ffmpeg", Status: "disabled", Detail: "ffmpeg is not installed; extended media is disabled"}
	}
	if _, err := probe.CommandOut(ctx, path, "-version"); err != nil {
		return Check{Name: "ffmpeg", Status: "disabled", Detail: "ffmpeg is not runnable; extended media is disabled"}
	}
	return Check{Name: "ffmpeg", Status: "ok", Detail: "ffmpeg is available"}
}

func majorVersion(text string) int {
	fields := strings.Fields(text)
	for i := len(fields) - 1; i >= 0; i-- {
		part := strings.Trim(fields[i], "()")
		head, _, _ := strings.Cut(part, ".")
		n, err := strconv.Atoi(head)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func queryMigrationState(ctx context.Context, databaseURL string) (bool, bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(queryCtx, databaseURL)
	if err != nil {
		return false, false, errDatabaseUnavailable
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var posts, jobs bool
	err = conn.QueryRow(queryCtx, `
SELECT to_regclass('public.posts') IS NOT NULL, to_regclass('public.jobs') IS NOT NULL`).Scan(&posts, &jobs)
	if err != nil {
		return false, false, errDatabaseUnavailable
	}
	return posts, jobs, nil
}

var errDatabaseUnavailable = errors.New("database is unavailable")

func writableDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errNotWritable
	}
	file, err := os.CreateTemp(dir, ".kura-preflight-*")
	if err != nil {
		return errNotWritable
	}
	name := file.Name()
	_ = file.Close()
	if err := os.Remove(name); err != nil {
		return errNotWritable
	}
	return nil
}

var errNotWritable = errors.New("directory is not writable")
