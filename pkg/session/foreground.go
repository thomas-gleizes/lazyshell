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
// Goes through SyscallConn().Control rather than the shorter
// unix.IoctlGetInt(int(ptmx.Fd()), ...): File.Fd() reaches into the file's
// internal descriptor without taking its lock, so it races the drain
// goroutine's io.Copy on the same *os.File — and, worse, could hand the ioctl
// a descriptor that Kill closed a moment later, by which point the number may
// already name something else entirely.
//
// Control holds that lock for the duration of the callback, which is exactly
// the guarantee this needs. It matters more since the resources tab started
// sampling in the background: before, this was only called from the drain
// goroutine and its own timer, and now every session is asked for its
// foreground group on a third one.
func foregroundPGID(ptmx *os.File) (int, error) {
	conn, err := ptmx.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("session: syscall conn: %w", err)
	}

	var (
		pgid     int
		ioctlErr error
	)

	if err := conn.Control(func(fd uintptr) {
		pgid, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	}); err != nil {
		return 0, fmt.Errorf("session: control: %w", err)
	}

	if ioctlErr != nil {
		return 0, fmt.Errorf("session: tcgetpgrp: %w", ioctlErr)
	}

	return pgid, nil
}
