//go:build !windows

package recovery

import (
	"os/exec"
	"syscall"
)

// Stop the entire tool tree on cancellation, including an active pg_dump.
func configureBackupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
