package session

import (
	"testing"
	"time"
)

// oscCommandExit builds the OSC 133 sequence a shell with integration wired
// up would print around a command that finished with the given exit code —
// the same technique pkg/gui's command_exit_indicator_test.go uses to
// simulate a command finishing without depending on the real shell process
// or an actual shell-integration setup.
func oscCommandExit(code int) []byte {
	if code == 0 {
		return []byte("\x1b]133;A\x07$ \x1b]133;B\x07true\r\n\x1b]133;D;0\x07")
	}

	return []byte("\x1b]133;A\x07$ \x1b]133;B\x07false\r\n\x1b]133;D;1\x07")
}

// oscShellStartupNoise builds the OSC 133 sequence a real shell's own
// precmd hook prints before its very first prompt, with no command having
// run yet: a D closing a cycle that never had a B or C in it (so it never
// sets LastCommandOutputRange), then the A opening the real, first prompt.
// zsh/bash's standard integration hooks fire precmd once at shell startup,
// unconditionally, before reading any input — this is what makes "the very
// first D event" the wrong signal for "the injected command's own exit".
func oscShellStartupNoise() []byte {
	return []byte("\x1b]133;D;0\x07\x1b]133;A\x07")
}

// newTestManagerWithStopOnFailure is newTestManager with
// StopOnFailurePollInterval shrunk far enough that watchStopOnFailure's
// reaction to a simulated OSC 133 event can be observed inside a normal test
// deadline.
func newTestManagerWithStopOnFailure(t *testing.T) *Manager {
	t.Helper()

	m := newTestManager(t)
	m.StopOnFailurePollInterval = 20 * time.Millisecond

	return m
}

func TestStopOnFailureKillsSessionWhenInjectedCommandFails(t *testing.T) {
	m := newTestManagerWithStopOnFailure(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Command: "true", StopOnFailure: true})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Screen().Write(oscCommandExit(1)); err != nil {
		t.Fatalf("Screen().Write: %v", err)
	}

	waitForStatus(t, sess, StatusExited)
}

func TestStopOnFailureLeavesSessionRunningOnSuccess(t *testing.T) {
	m := newTestManagerWithStopOnFailure(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Command: "true", StopOnFailure: true})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Screen().Write(oscCommandExit(0)); err != nil {
		t.Fatalf("Screen().Write: %v", err)
	}

	time.Sleep(10 * m.StopOnFailurePollInterval)

	if sess.Status() != StatusRunning {
		t.Errorf("Status() = %v, want StatusRunning: a successful command must never be killed", sess.Status())
	}
}

// Regression test for the default: stop_on_failure not declared must leave
// today's "the shell is still there" behaviour untouched even when the
// command fails.
func TestStopOnFailureDisabledByDefaultLeavesSessionRunning(t *testing.T) {
	m := newTestManagerWithStopOnFailure(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Command: "true"})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Screen().Write(oscCommandExit(1)); err != nil {
		t.Fatalf("Screen().Write: %v", err)
	}

	time.Sleep(10 * m.StopOnFailurePollInterval)

	if sess.Status() != StatusRunning {
		t.Errorf("Status() = %v, want StatusRunning: StopOnFailure defaults to off", sess.Status())
	}
}

// A real shell's own precmd hook fires once at startup, before the injected
// command ever runs, closing a cycle with no command in it — a D event with
// no LastCommandOutputRange. watchStopOnFailure must skip this spurious
// event and react to the injected command's own D instead, not treat this
// boilerplate one as "the" event to observe and then stop watching.
func TestStopOnFailureSkipsShellStartupNoiseBeforeInjectedCommand(t *testing.T) {
	m := newTestManagerWithStopOnFailure(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Command: "true", StopOnFailure: true})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Screen().Write(oscShellStartupNoise()); err != nil {
		t.Fatalf("Screen().Write (shell startup noise): %v", err)
	}

	time.Sleep(5 * m.StopOnFailurePollInterval)

	if sess.Status() != StatusRunning {
		t.Fatalf("Status() after shell startup noise = %v, want StatusRunning: "+
			"a D with no command output range must never be mistaken for the injected command's own",
			sess.Status())
	}

	if _, err := sess.Screen().Write(oscCommandExit(1)); err != nil {
		t.Fatalf("Screen().Write (injected command failure): %v", err)
	}

	waitForStatus(t, sess, StatusExited)
}

// watchStopOnFailure only ever acts on the first D event that closes a cycle
// which actually ran a command — the injected command's own. A second, later
// one (standing in for a command the user types by hand once the shell is
// theirs again) must never trigger a kill.
func TestStopOnFailureOnlyWatchesFirstCommandExit(t *testing.T) {
	m := newTestManagerWithStopOnFailure(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Command: "true", StopOnFailure: true})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Screen().Write(oscCommandExit(0)); err != nil {
		t.Fatalf("Screen().Write (first, success): %v", err)
	}

	time.Sleep(10 * m.StopOnFailurePollInterval)

	if sess.Status() != StatusRunning {
		t.Fatalf("Status() after first (successful) command = %v, want StatusRunning", sess.Status())
	}

	if _, err := sess.Screen().Write(oscCommandExit(1)); err != nil {
		t.Fatalf("Screen().Write (second, failure): %v", err)
	}

	time.Sleep(10 * m.StopOnFailurePollInterval)

	if sess.Status() != StatusRunning {
		t.Errorf("Status() after second (failed) command = %v, want StatusRunning: "+
			"only the injected command's own exit may trigger a kill", sess.Status())
	}
}

// A killed-on-failure session must not resurrect itself even if it also
// declares restart: on-failure — Manager.Kill's killedExplicitly is what
// suppresses WillAutoRestart, exactly like a manual "x" would.
func TestStopOnFailureWithRestartOnFailureDoesNotResurrect(t *testing.T) {
	m := newTestManagerWithRestart(t)
	m.StopOnFailurePollInterval = 20 * time.Millisecond

	sess, err := m.NewWithOptions(Options{
		Name: "api", Shell: testShell, Command: "true",
		StopOnFailure: true, Restart: RestartOnFailure,
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Screen().Write(oscCommandExit(1)); err != nil {
		t.Fatalf("Screen().Write: %v", err)
	}

	waitForStatus(t, sess, StatusExited)

	time.Sleep(m.RestartBackoffMax + 200*time.Millisecond)

	got, ok := m.Get(sess.ID)
	if !ok || got != sess || got.Status() != StatusExited {
		t.Errorf("a stop_on_failure kill was auto-restarted anyway; Get() = %+v", got)
	}
}
