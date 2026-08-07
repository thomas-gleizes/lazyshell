package session

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// foregroundPGID reports the process group currently in the foreground of
// ptmx — the group the terminal would deliver a Ctrl-C to, i.e. whatever the
// user is actually looking at rather than the shell that spawned it.
//
// The ioctl is the one part of the foreground lookup that is identical on
// every Unix; what differs is how a pgid is turned into a name (see
// foreground_darwin.go / foreground_linux.go) or into resource usage (see
// stats_darwin.go / stats_linux.go). Both need it, hence this file.
func foregroundPGID(ptmx *os.File) (int, error) {
	pgid, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return 0, fmt.Errorf("session: tcgetpgrp: %w", err)
	}

	return pgid, nil
}
