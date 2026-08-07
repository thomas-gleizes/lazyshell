//go:build darwin

package session

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// foregroundProcessName is foreground_linux.go's macOS counterpart: same
// TIOCGPGRP ioctl (there is no /proc on Darwin), then a KERN_PROC_PID sysctl
// for the process group leader's name.
func foregroundProcessName(ptmx *os.File) (string, error) {
	pgid, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return "", fmt.Errorf("session: tcgetpgrp: %w", err)
	}

	info, err := unix.SysctlKinfoProc("kern.proc.pid", pgid)
	if err != nil {
		return "", fmt.Errorf("session: sysctl kern.proc.pid %d: %w", pgid, err)
	}

	comm := info.Proc.P_comm[:]
	if i := bytes.IndexByte(comm, 0); i >= 0 {
		comm = comm[:i]
	}

	return string(comm), nil
}
