//go:build windows

package recovery

import "os"

// Windows MoveFile semantics used by os.Rename refuse an existing destination.
func replicaRenameNoReplace(source, destination string) error {
	return os.Rename(source, destination)
}
