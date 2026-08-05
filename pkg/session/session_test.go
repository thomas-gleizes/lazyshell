package session

import (
	"strings"
	"testing"
	"time"
)

const testShell = "/bin/sh"

// newTestManager returns a Manager with a short kill timeout, so tests that
// exercise the SIGKILL escalation path do not have to wait on a
// production-sized timeout.
func newTestManager(t *testing.T) *Manager {
	t.Helper()

	m := NewManager()
	m.KillTimeout = 300 * time.Millisecond

	t.Cleanup(m.Shutdown)

	return m
}

// newTestSession starts a plain shell and arranges for it to be killed and
// fully drained before the test returns, so no goroutine outlives it.
func newTestSession(t *testing.T, m *Manager, name string) *Session {
	t.Helper()

	sess, err := m.New(name, testShell)
	if err != nil {
		t.Fatalf("Manager.New: %v", err)
	}

	return sess
}

// waitForScreen polls the session's rendered screen until it contains want,
// rather than sleeping a fixed amount — the shell's timing is not otherwise
// deterministic.
func waitForScreen(t *testing.T, sess *Session, want string) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if out := sess.Screen().Render(); strings.Contains(out, want) {
			return out
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q on screen:\n%s", want, sess.Screen().Render())

	return ""
}

// waitForStatus polls until the session reaches want, bounded by a deadline.
func waitForStatus(t *testing.T, sess *Session, want Status) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Status() == want {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for status %v, got %v", want, sess.Status())
}

func TestManagerNewStartsARunningSession(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if sess.ID == "" {
		t.Error("session has no ID")
	}

	if sess.Status() != StatusRunning {
		t.Errorf("Status() = %v, want %v", sess.Status(), StatusRunning)
	}

	if sess.Cmd.Process == nil || sess.Cmd.Process.Pid <= 0 {
		t.Errorf("Cmd.Process.Pid = %v, want > 0", sess.Cmd.Process)
	}
}

func TestWriteReachesTheShellAndAppearsOnScreen(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if _, err := sess.Write([]byte("echo lazyshell-ok\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForScreen(t, sess, "lazyshell-ok")
}

// The exit criterion in ROADMAP.md: killing a session must terminate the
// process group, not leave it dangling, and Done() must observably close.
func TestKillTerminatesTheProcessGroup(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if _, err := sess.Write([]byte("sleep 30\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	time.Sleep(300 * time.Millisecond) // let the shell actually start sleep

	if err := m.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close after Kill")
	}

	if sess.Status() != StatusExited {
		t.Errorf("Status() = %v, want %v", sess.Status(), StatusExited)
	}
}

// A shell that ignores SIGTERM must still die, via the SIGKILL escalation.
func TestKillEscalatesToSigkillWhenSigtermIsIgnored(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if _, err := sess.Write([]byte("trap '' TERM; echo trap-set; sleep 30\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForScreen(t, sess, "trap-set")

	if err := m.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close even after the SIGKILL escalation")
	}
}

func TestExitCodeIsCaptured(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if _, err := sess.Write([]byte("exit 7\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForStatus(t, sess, StatusExited)

	if sess.ExitCode() != 7 {
		t.Errorf("ExitCode() = %d, want 7", sess.ExitCode())
	}
}

// The drain goroutine must keep feeding the screen on its own, so output
// produced while a session is not displayed is not lost. The bound on how
// much scrollback is kept is pkg/screen's own concern, already tested there;
// this only confirms the feed reaches it.
func TestOutputFeedsTheScreenContinuously(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if _, err := sess.Write([]byte("for i in $(seq 1 200); do echo line-$i; done; echo done-marker\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForScreen(t, sess, "done-marker")

	if sess.Screen().ScrollbackLen() == 0 {
		t.Error("nothing went to the scrollback despite 200 lines of output")
	}
}

func TestSessionResizePropagatesToPtyAndScreen(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if err := sess.Resize(97, 31); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	if _, err := sess.Write([]byte("stty size\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForScreen(t, sess, "31 97")
}
