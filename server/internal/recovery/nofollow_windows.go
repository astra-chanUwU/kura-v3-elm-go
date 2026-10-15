//go:build windows

package recovery

// Windows Go has no syscall.O_NOFOLLOW. Callers Lstat and reject a symlink
// before opening the same path.
const openNoFollow = 0
