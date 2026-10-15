//go:build linux

package recovery

import (
	"runtime"
	"syscall"
	"unsafe"
)

// replicaRenameNoReplace uses Linux renameat2 so publication fails atomically
// if any path already occupies the final backup ID.
func replicaRenameNoReplace(source, destination string) error {
	src, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	const atFDCWD = ^uintptr(99) // -100
	const renameNoReplace = 1
	// amd64's syscall table omits the symbolic constant; arm64 defines it in
	// syscall.SYS_RENAMEAT2. Other Linux architectures fail closed here.
	var number uintptr
	switch runtime.GOARCH {
	case "amd64":
		number = 316
	case "arm64":
		number = 276
	default:
		return syscall.ENOSYS
	}
	_, _, errno := syscall.Syscall6(
		number,
		atFDCWD, uintptr(unsafe.Pointer(src)), atFDCWD,
		uintptr(unsafe.Pointer(dst)), renameNoReplace, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}
