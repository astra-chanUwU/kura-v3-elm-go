package preflight

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidPreflightOmitsSecrets(t *testing.T) {
	secret := "super-secret-password"
	report := Evaluate(context.Background(), readyProbe(secret))
	text := render(report)
	if !report.OK || strings.Contains(text, secret) || strings.Contains(text, "postgres://") {
		t.Fatalf("report leaked or failed: ok=%v\n%s", report.OK, text)
	}
	if !strings.Contains(text, "ok\tdatabase_connectivity\treachable") || !strings.Contains(text, "ok\tmigrations\tschema matches this release") || !strings.Contains(text, "disabled\tffmpeg\t") {
		t.Fatalf("report = %s", text)
	}
}

func TestMissingBackupToolFails(t *testing.T) {
	probe := readyProbe("secret")
	probe.LookPath = func(name string) (string, error) {
		if name == "pg_dump" {
			return "", errors.New("missing")
		}
		if name == "ffmpeg" {
			return "", errors.New("missing")
		}
		return name, nil
	}
	report := Evaluate(context.Background(), probe)
	if report.OK || !has(report, "backup_tools", "fail") || !has(report, "ffmpeg", "disabled") {
		t.Fatalf("report = %+v", report.Checks)
	}
}

func TestUnwritableRootFails(t *testing.T) {
	probe := readyProbe("secret")
	probe.Getenv = func(key string) string {
		env := validEnv("secret")
		if key == "MEDIA_ROOT" {
			return `C:\not-a-url`
		}
		return env(key)
	}
	probe.Writable = func(dir string) error {
		if strings.Contains(dir, "not-a-url") {
			return errors.New("denied")
		}
		return nil
	}
	report := Evaluate(context.Background(), probe)
	if report.OK || !has(report, "media_root", "fail") || strings.Contains(render(report), "denied") {
		t.Fatalf("report = %s", render(report))
	}
}

func TestUnavailableDatabaseFailsClosed(t *testing.T) {
	probe := readyProbe("secret")
	probe.Database = func(context.Context, string) (bool, bool, error) {
		return false, false, errors.New("postgres://kura:secret@127.0.0.1/db dial tcp")
	}
	text := render(Evaluate(context.Background(), probe))
	if !strings.Contains(text, "fail\tdatabase_connectivity\tdatabase is unavailable") || strings.Contains(text, "secret") || strings.Contains(text, "dial tcp") {
		t.Fatalf("report = %s", text)
	}
}

func TestOptionalFFmpegDisabledStillPasses(t *testing.T) {
	report := Evaluate(context.Background(), readyProbe("secret"))
	if !report.OK || !has(report, "ffmpeg", "disabled") || !has(report, "backup_schedule", "disabled") {
		t.Fatalf("report = %+v", report.Checks)
	}
}

func TestSchemaBehindReleaseIsNotSeeded(t *testing.T) {
	probe := readyProbe("secret")
	probe.Database = func(context.Context, string) (bool, bool, error) {
		return true, false, nil
	}
	text := render(Evaluate(context.Background(), probe))
	if !strings.Contains(text, "fail\tmigrations\tschema is behind this release") || strings.Contains(text, "ok\tmigrations") || strings.Contains(text, "secret") {
		t.Fatalf("report = %s", text)
	}
}

func TestMissingPythonFailsAndLeavesFFmpegOptional(t *testing.T) {
	probe := readyProbe("secret")
	probe.LookPath = func(name string) (string, error) {
		if name == "python3" || name == "python" || name == "ffmpeg" {
			return "", errors.New("missing")
		}
		return name, nil
	}
	report := Evaluate(context.Background(), probe)
	if report.OK || !has(report, "backup_tools", "fail") || !has(report, "ffmpeg", "disabled") {
		t.Fatalf("report = %+v", report.Checks)
	}
	if strings.Contains(render(report), "secret") {
		t.Fatal("report leaked the database secret")
	}
}

func TestOldPythonFailsWithoutToolOutput(t *testing.T) {
	probe := readyProbe("secret")
	probe.CommandOut = func(_ context.Context, name string, _ ...string) (string, error) {
		if strings.Contains(strings.ToLower(name), "python") {
			return "Python 3.8.2 postgres://kura:secret@127.0.0.1/db", nil
		}
		return "pg_dump (PostgreSQL) 16.11", nil
	}
	text := render(Evaluate(context.Background(), probe))
	if !strings.Contains(text, "fail\tbackup_tools\tPython 3.9 or newer is required") || strings.Contains(text, "secret") || strings.Contains(text, "3.8") || strings.Contains(text, "postgres://") {
		t.Fatalf("report = %s", text)
	}
}

func TestUnmigratedDatabaseIsNotSeeded(t *testing.T) {
	probe := readyProbe("secret")
	probe.Database = func(context.Context, string) (bool, bool, error) {
		return false, false, nil
	}
	text := render(Evaluate(context.Background(), probe))
	if !strings.Contains(text, "has not been migrated") || strings.Contains(text, "seed") && strings.Contains(text, "ok\tmigrations") {
		t.Fatalf("report = %s", text)
	}
}

func readyProbe(secret string) Probe {
	return Probe{
		Getenv: validEnv(secret),
		LookPath: func(name string) (string, error) {
			if name == "ffmpeg" {
				return "", errors.New("missing")
			}
			return name, nil
		},
		CommandOut: func(_ context.Context, name string, _ ...string) (string, error) {
			if strings.Contains(strings.ToLower(name), "python") {
				return "Python 3.12.3", nil
			}
			return "pg_dump (PostgreSQL) 16.11", nil
		},
		Database: func(context.Context, string) (bool, bool, error) {
			return true, true, nil
		},
		Writable: func(string) error { return nil },
	}
}

func validEnv(secret string) func(string) string {
	values := map[string]string{
		"DATABASE_URL":             "postgres://kura:" + secret + "@127.0.0.1:5432/kura?sslmode=disable",
		"MEDIA_ROOT":               `C:\library\media`,
		"KURA_BACKUP_ROOT":         `C:\library\backups`,
		"KURA_BACKUP_INTERVAL":     "",
		"KURA_BACKUP_KEEP":         "",
		"KURA_BACKUP_REPLICA_ROOT": "",
	}
	return func(key string) string { return values[key] }
}

func render(report Report) string {
	var buf bytes.Buffer
	for _, check := range report.Checks {
		buf.WriteString(check.Status + "\t" + check.Name + "\t" + check.Detail + "\n")
	}
	return buf.String()
}

func has(report Report, name, status string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
