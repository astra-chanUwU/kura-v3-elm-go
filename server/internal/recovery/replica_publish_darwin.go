//go:build darwin

package recovery

import (
	"syscall"
	"unsafe"
)

// renameatx_np with RENAME_EXCL publishes the staged directory only when the
// destination does not exist, including when another process races the check.
func replicaRenameNoReplace(source, destination string) error {
	src, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	const atFDCWD = ^uintptr(1) // -2
	const renameAtxNP = 488
	const renameExclusive = 0x4
	_, _, errno := syscall.Syscall6(
		renameAtxNP,
		atFDCWD, uintptr(unsafe.Pointer(src)), atFDCWD,
		uintptr(unsafe.Pointer(dst)), renameExclusive, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}
