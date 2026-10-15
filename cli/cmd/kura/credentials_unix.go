//go:build !windows

package main

import (
	"fmt"
	"os"
)

func restrictFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict credential file: %w", err)
	}
	return nil
}

func restrictDir(path string) error {
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("restrict credential directory: %w", err)
	}
	return nil
}

func assertUserPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("account session file must not be a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("account session file is accessible to other users")
	}
	return nil
}
