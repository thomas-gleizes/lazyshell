package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/thomas-gleizes/lazyshell/pkg/screen"
)

// Status is the lifecycle state of a Session.
type Status int

const (
	// StatusRunning is the state from creation until the shell process exits.
	StatusRunning Status = iota
	// StatusExited means the shell process has been reaped; ExitCode is
	// meaningful from this point on.
	StatusExited
)

func (s Status) String() string {
	if s == StatusExited {
		return "exited"
	}

	return "running"
}

// defaultCols/defaultRows are the geometry a session starts with, before the
// GUI (phase 3+) resizes it from the actual panel dimensions — same default
// as the phase 1 spike.
const (
	defaultCols = 80
	defaultRows = 24
)

// Session owns one shell process behind a pty, together with the terminal
// emulator that renders its output. It keeps running independently of
// whether it is currently displayed: a drain goroutine, started at creation
// and alive for the life of the process, continuously feeds the emulator so
// no output is ever lost.
type Session struct {
	ID        string
	Name      string
	Cmd       *exec.Cmd
	Cwd       string
	CreatedAt time.Time

	ptmx   *os.File
	screen *screen.Screen

	// mu guards status, exitCode, cols and rows: status/exitCode are written
	// by the drain goroutine and read from whichever goroutine owns the GUI;
	// cols/rows are written by Resize, which the roadmap expects to be called
	// from the layout pass, again a different goroutine than drain's.
	mu       sync.Mutex
	status   Status
	exitCode int
	cols     int
	rows     int

	// done is closed exactly once, by drain, once the process has been
	// reaped and both copy goroutines have returned. Kill waits on it, bounded
	// by a timeout; callers that need to know a session is fully gone (tests,
	// Manager.Shutdown) do the same via Done().
	done chan struct{}

	killOnce sync.Once
}

// Status reports the session's current lifecycle state.
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status
}

// ExitCode is meaningful once Status returns StatusExited; -1 if the process
// was killed by a signal rather than exiting normally.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exitCode
}

// Screen exposes the session's terminal emulator, for rendering. Screen
// itself is already safe for concurrent use.
func (s *Session) Screen() *screen.Screen {
	return s.screen
}

// Done is closed once the session has fully terminated: process reaped, both
// copy goroutines returned. Waiting on it is how a caller confirms nothing of
// this session is left running.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Write sends keyboard input to the shell.
func (s *Session) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

// Resize propagates a new geometry to both the pty and the emulator, mirror
// of what cmd/spike-pty's resize does. cols/rows <= 0 are ignored: gocui
// reports a transient zero size during some layout passes.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}

	s.mu.Lock()
	if cols == s.cols && rows == s.rows {
		s.mu.Unlock()

		return nil
	}
	s.mu.Unlock()

	if err := pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}); err != nil {
		return fmt.Errorf("session: pty.Setsize: %w", err)
	}

	s.screen.Resize(cols, rows)

	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()

	return nil
}

// drain feeds the pty's output into the emulator, and the emulator's answers
// back into the pty, until the shell is gone; it then reaps the process and
// records its exit status. It is started once per session, at creation, and
// runs independently of anything that reads Screen() for display.
func (s *Session) drain() {
	defer close(s.done)

	answersDone := make(chan struct{})
	go func() {
		defer close(answersDone)
		// Terminal capability queries (DA/CPR/OSC colour...) are answered by
		// the emulator; without this, a shell that asks a question waits for
		// a reply that never comes. Unblocked by s.screen.Close() below.
		_, _ = io.Copy(s.ptmx, s.screen)
	}()

	// Runs until the pty goes away: either the shell exited on its own, or
	// Kill closed s.ptmx to force this Read to return.
	_, _ = io.Copy(s.screen, s.ptmx)

	// Nothing will write to s.screen again; release the answer-reader
	// goroutine explicitly, or it leaks for the rest of the process — this is
	// exactly what a leak-conscious test run would need to catch.
	_ = s.screen.Close()
	<-answersDone

	exitCode := 0
	if err := s.Cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	s.mu.Lock()
	s.status, s.exitCode = StatusExited, exitCode
	s.mu.Unlock()
}

// Kill terminates the whole process group (so children the shell spawned,
// e.g. a foreground vim or sleep, die too, not just the shell itself) and
// waits up to timeout for it to be reaped. If it is still alive after that,
// it escalates to SIGKILL and closes the pty as a last resort, then waits up
// to timeout again.
//
// A backgrounded job that gave itself its own process group via shell job
// control (`cmd &`) can survive this — a known, accepted gap, not solved here.
func (s *Session) Kill(timeout time.Duration) (err error) {
	s.killOnce.Do(func() {
		if s.Status() == StatusExited {
			return
		}

		// pty.StartWithSize always sets Setsid on the shell, so its pid is
		// already the process group id.
		pgid := s.Cmd.Process.Pid

		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		select {
		case <-s.done:
			return
		case <-time.After(timeout):
		}

		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = s.ptmx.Close()

		select {
		case <-s.done:
		case <-time.After(timeout):
			err = fmt.Errorf("session %s: still running after SIGKILL", s.ID)
		}
	})

	return err
}
