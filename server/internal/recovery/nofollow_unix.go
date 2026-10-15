//go:build !windows

package recovery

import "syscall"

const openNoFollow = syscall.O_NOFOLLOW
