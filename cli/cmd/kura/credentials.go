package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/cli/internal/client"
)

const sessionFileEnv = "KURA_SESSION_FILE"

type storedSession struct {
	Version    int       `json:"version"`
	APIURL     string    `json:"api_url"`
	CookieName string    `json:"cookie_name"`
	Cookie     string    `json:"cookie"`
	ExpiresAt  time.Time `json:"expires_at"`
	Handle     string    `json:"handle"`
	Role       string    `json:"role"`
}

func (s storedSession) expired(now time.Time) bool {
	return !s.ExpiresAt.After(now)
}

func (s storedSession) validate() error {
	if s.Version != 1 {
		return fmt.Errorf("account session file is unusable")
	}
	if !client.ValidSessionCookie(s.CookieName, s.Cookie) {
		return fmt.Errorf("account session file is unusable")
	}
	if err := client.ValidateBaseURL(s.APIURL); err != nil {
		return fmt.Errorf("account session file is unusable")
	}
	if s.ExpiresAt.IsZero() {
		return fmt.Errorf("account session file is unusable")
	}
	switch s.Role {
	case "viewer", "editor", "admin":
	default:
		return fmt.Errorf("account session file is unusable")
	}
	if strings.TrimSpace(s.Handle) == "" || strings.ContainsAny(s.Handle, " \r\n\t") {
		return fmt.Errorf("account session file is unusable")
	}
	return nil
}

func defaultSessionFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a private credential file")
	}
	return filepath.Join(dir, "kura", "credentials.json"), nil
}

// withDefaultSessionFile fills KURA_SESSION_FILE for the real process when
// the operator did not choose a path. Tests pass their own getenv and do not
// use this wrapper, so they never touch the user profile.
func withDefaultSessionFile(getenv func(string) string) func(string) string {
	return func(key string) string {
		if getenv == nil {
			getenv = func(string) string { return "" }
		}
		if key == sessionFileEnv && strings.TrimSpace(getenv(key)) == "" {
			if path, err := defaultSessionFile(); err == nil {
				return path
			}
		}
		return getenv(key)
	}
}

func sessionFileFromEnv(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	return strings.TrimSpace(getenv(sessionFileEnv))
}

func assertNoSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("account session file must not be a symlink")
	}
	return nil
}

func readStoredSession(path string) (storedSession, error) {
	if strings.TrimSpace(path) == "" {
		return storedSession{}, os.ErrNotExist
	}
	if err := assertNoSymlink(path); err != nil {
		return storedSession{}, err
	}
	if err := assertUserPrivate(path); err != nil {
		return storedSession{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storedSession{}, err
	}
	var session storedSession
	if err := json.Unmarshal(data, &session); err != nil {
		return storedSession{}, fmt.Errorf("account session file is unusable")
	}
	session.APIURL = strings.TrimRight(strings.TrimSpace(session.APIURL), "/")
	if err := session.validate(); err != nil {
		return storedSession{}, err
	}
	return session, nil
}

func writeStoredSession(path string, session storedSession) error {
	session.APIURL = strings.TrimRight(strings.TrimSpace(session.APIURL), "/")
	if err := session.validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("encode account session")
	}
	data = append(data, '\n')
	return writePrivateFile(path, data)
}

func removeStoredSession(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := assertNoSymlink(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writePrivateFile(path string, data []byte) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no credential file configured")
	}
	if err := rejectSymlink(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	if err := rejectSymlink(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create credential directory: %w", err)
		}
		if err := restrictDir(dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return fmt.Errorf("create credential file: %w", err)
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict credential file: %w", err)
	}
	if err := restrictFile(tmpName); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write credential file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write credential file: %w", err)
	}
	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("publish credential file: %w", err)
	}
	success = true
	if err := restrictFile(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return assertUserPrivate(path)
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("credential path must not be a symlink")
	}
	return nil
}

func replaceFile(tmpName, path string) error {
	if err := os.Rename(tmpName, path); err == nil {
		return nil
	}
	if err := assertNoSymlink(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmpName, path)
}
