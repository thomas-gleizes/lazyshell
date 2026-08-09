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

// waitForUnwrappedScreen is waitForScreen for a value that can be wider than
// the 80-column test screen — a temp directory path, typically.
//
// The emulator hard-wraps such a value across two rows with no marker, so a
// plain Contains never matches it: the test then fails for a reason that has
// nothing to do with what it is checking, and only on machines whose TMPDIR
// happens to be long. Joining the rows back together is what makes the check
// about the value rather than about the terminal's width.
func waitForUnwrappedScreen(t *testing.T, sess *Session, want string) {
	t.Helper()

	unwrap := func(s string) string { return strings.ReplaceAll(s, "\n", "") }

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(unwrap(sess.Screen().Render()), unwrap(want)) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q on screen (unwrapped):\n%s", want, sess.Screen().Render())
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

func TestSessionNameReflectsCreationName(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "t")

	if got := sess.Name(); got != "t" {
		t.Errorf("Name() = %q, want %q", got, "t")
	}
}

// SetName is the "renommage de session" ergonomics feature: purely
// cosmetic, it must not touch the running shell.
func TestSessionSetNameRenames(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "before")

	sess.SetName("after")

	if got := sess.Name(); got != "after" {
		t.Errorf("Name() after SetName = %q, want %q", got, "after")
	}
	if sess.Status() != StatusRunning {
		t.Errorf("Status() after SetName = %v, want %v (rename must not touch the shell)", sess.Status(), StatusRunning)
	}
}

// A session starts in Options.Group, and SetGroup moves it — including back
// out of every group, which "" means and which the panel's "g" key submits as
// an empty prompt. Like a rename, it must not touch the running shell.
func TestSessionGroupStartsFromOptionsAndIsReassignable(t *testing.T) {
	m := newTestManager(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Group: "backend"})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if got := sess.Group(); got != "backend" {
		t.Errorf("Group() = %q, want the Options group %q", got, "backend")
	}

	sess.SetGroup("agents")
	if got := sess.Group(); got != "agents" {
		t.Errorf("Group() after SetGroup = %q, want %q", got, "agents")
	}

	sess.SetGroup("")
	if got := sess.Group(); got != "" {
		t.Errorf("Group() after SetGroup(\"\") = %q, want the session ungrouped", got)
	}

	if sess.Status() != StatusRunning {
		t.Errorf("Status() after SetGroup = %v, want %v (grouping must not touch the shell)", sess.Status(), StatusRunning)
	}
}

// A session created without a Group is ungrouped — the state every session in
// a lazyshell with no project file is in, and the one the panel must render
// exactly as it did before groups existed.
func TestSessionWithoutOptionsGroupIsUngrouped(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "s")

	if got := sess.Group(); got != "" {
		t.Errorf("Group() = %q, want \"\" for a session created with no group", got)
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

	if _, err := sess.Write([]byte("for i in $(seq 1 200); do echo line-$i; done\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The marker must be something the pty's echo of the command line cannot
	// contain, or this returns immediately — before the loop has produced
	// anything — and the scrollback assertion below fails under load. "line-200"
	// only ever exists as loop output; "$i" is what appears in the command.
	waitForScreen(t, sess, "line-200")

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
