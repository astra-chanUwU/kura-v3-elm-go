//go:build !linux && !windows && !darwin

package recovery

import "errors"

// Fail closed where this package has no verified atomic no-replace operation.
func replicaRenameNoReplace(_, _ string) error {
	return errors.New("atomic no-replace publication is unsupported on this platform")
}
