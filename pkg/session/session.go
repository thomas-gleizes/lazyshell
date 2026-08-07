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

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/screen"
)

// agentCheckInterval bounds how often a session re-evaluates its AI agent
// state, per the design report's rendering budget: detection runs in the
// drain goroutine, never the render loop, and at most this often — cheap
// enough that a chatty session (a busy `claude` streaming tokens) does not
// turn into a hidden CPU cost.
const agentCheckInterval = 500 * time.Millisecond

// agentScreenTailLines is how many rows of the visible screen a manifest's
// screen_pattern is evaluated against — enough to catch a prompt at the
// bottom of the screen without scanning the whole scrollback on every check.
const agentScreenTailLines = 20

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
	Cmd       *exec.Cmd
	Cwd       string
	CreatedAt time.Time

	// opts is the Options this session was created from, kept so Restart can
	// spawn a fresh process with the same shell, cwd, env and initial command
	// — killOnce means the object that just exited can never run again.
	opts Options

	ptmx   *os.File
	screen *screen.Screen

	// detector is the shared *agent.Detector every session in the same
	// Manager evaluates against (pkg/session/manager.go). Nil-safe: a nil
	// Detector's Evaluate always answers agent.StateNone, so a caller that
	// never wired one (most tests, and any Manager built as a bare struct
	// literal) gets "not an agent session" rather than a crash.
	detector *agent.Detector

	// mu guards name, status, exitCode, cols, rows, agentState and the
	// agent-recheck bookkeeping below: name is written by SetName (the
	// "renommage de session" ergonomics feature, phase 5) from gocui's main
	// goroutine and read from sessionsPanelContent on goEvery's background
	// goroutine; status/exitCode are written by the drain goroutine and read
	// from whichever goroutine owns the GUI; cols/rows are written by
	// Resize, which the roadmap expects to be called from the layout pass,
	// again a different goroutine than drain's; agentState and
	// lastAgentCheck/agentRecheckPending are touched from both the drain
	// goroutine and the deferred timer evaluateAgentState arms below.
	mu         sync.Mutex
	name       string
	status     Status
	exitCode   int
	cols       int
	rows       int
	agentState agent.State

	// lastAgentCheck/agentRecheckPending implement evaluateAgentState's
	// trailing-edge throttle: a check arriving inside agentCheckInterval of
	// the last one is not just dropped, or the last burst of a turn — a
	// permission prompt appearing right before an agent goes quiet waiting
	// for the user, exactly the state this feature exists to catch — could
	// be silently missed forever on a session that produces no further
	// output. Instead exactly one deferred re-check is armed for when the
	// window closes, so the state written just before the session goes idle
	// still lands.
	lastAgentCheck      time.Time
	agentRecheckPending bool

	// done is closed exactly once, by drain, once the process has been
	// reaped and both copy goroutines have returned. Kill waits on it, bounded
	// by a timeout; callers that need to know a session is fully gone (tests,
	// Manager.Shutdown) do the same via Done().
	done chan struct{}

	killOnce sync.Once
}

// Name reports the session's current display name.
func (s *Session) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.name
}

// SetName changes the session's display name — the "renommage de session"
// ergonomics feature. Purely cosmetic: it does not touch the running shell.
func (s *Session) SetName(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
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

// AgentState is the last AI agent state detected for this session's
// foreground process (pkg/agent) — agent.StateNone for a session that is not
// running a known agent. Safe to call from any goroutine; see the mu doc
// comment on Session's fields.
func (s *Session) AgentState() agent.State {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.agentState
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

// agentEvalWriter forwards to a Session's screen, then triggers a throttled
// AI agent state re-check — the drain loop's only extra step over a plain
// io.Copy(s.screen, s.ptmx), and kept as a separate io.Writer specifically so
// drain can still hand io.Copy the exact destination shape it had before
// (see the comment on io.Copy(agentEvalWriter{s}, s.ptmx) in drain).
type agentEvalWriter struct {
	s *Session
}

func (w agentEvalWriter) Write(p []byte) (int, error) {
	n, err := w.s.screen.Write(p)
	if n > 0 {
		w.s.evaluateAgentState()
	}

	return n, err
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
	//
	// Written through agentEvalWriter rather than s.screen directly, so this
	// stays exactly io.Copy(s.screen, s.ptmx) in every way that matters — same
	// WriteTo/ReadFrom fast-path selection *os.File already gets — while
	// still getting a callback after each chunk actually reaches the screen,
	// which is what evaluateAgentState needs. A hand-rolled Read/Write loop
	// here instead of io.Copy was tried first and changed the timing enough
	// to break an existing scrollback test — io.Copy's fast path
	// (ptmx.WriteTo, not the generic buffered copy) turned out to matter.
	_, _ = io.Copy(agentEvalWriter{s}, s.ptmx)

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

// evaluateAgentState re-detects this session's AI agent state, throttled to
// agentCheckInterval and normally called only from drain's goroutine on
// every screen change — never from the render loop, per the design report's
// budget (TestIdleSessionDoesNotRepaint must stay unaffected by this).
//
// A call inside the throttle window is not simply dropped: it arms one
// deferred re-check for when the window closes (see the agentRecheckPending
// doc comment), so it can also run from that timer's own goroutine — the
// only two callers this method ever has.
//
// A detector-less Manager (most tests) or a foreground-process lookup
// failure (process already gone, unsupported platform) both degrade to
// agent.StateNone rather than erroring: an agent state is a gutter hint, not
// something anything else in lazyshell depends on.
func (s *Session) evaluateAgentState() {
	if s.detector == nil {
		return
	}

	s.mu.Lock()
	if elapsed := time.Since(s.lastAgentCheck); elapsed < agentCheckInterval {
		remaining := agentCheckInterval - elapsed
		pending := s.agentRecheckPending
		s.agentRecheckPending = true
		s.mu.Unlock()

		if !pending {
			time.AfterFunc(remaining, func() {
				s.mu.Lock()
				s.agentRecheckPending = false
				s.mu.Unlock()

				s.evaluateAgentState()
			})
		}

		return
	}
	s.lastAgentCheck = time.Now()
	s.mu.Unlock()

	name, err := foregroundProcessName(s.ptmx)
	if err != nil {
		s.mu.Lock()
		s.agentState = agent.StateNone
		s.mu.Unlock()

		return
	}

	tail := s.screen.PlainTail(agentScreenTailLines)
	title := s.screen.Title()
	state := s.detector.Evaluate(name, tail, title)

	s.mu.Lock()
	s.agentState = state
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
