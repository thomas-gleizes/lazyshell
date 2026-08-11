package session

import (
	"testing"
	"time"
)

func TestBackoffDelayDoublesUpToCap(t *testing.T) {
	base := 1 * time.Second
	max := 60 * time.Second

	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second,
		60 * time.Second,
	}

	for attempts, w := range want {
		if got := backoffDelay(base, max, attempts); got != w {
			t.Errorf("backoffDelay(attempts=%d) = %v, want %v", attempts, got, w)
		}
	}
}

func TestSessionWillAutoRestart(t *testing.T) {
	tests := []struct {
		name             string
		policy           RestartPolicy
		status           Status
		exitCode         int
		killedExplicitly bool
		want             bool
	}{
		{"never, failed", RestartNever, StatusExited, 1, false, false},
		{"on-failure, still running", RestartOnFailure, StatusRunning, 1, false, false},
		{"on-failure, exited zero", RestartOnFailure, StatusExited, 0, false, false},
		{"on-failure, exited nonzero", RestartOnFailure, StatusExited, 1, false, true},
		{"on-failure, exited nonzero but killed", RestartOnFailure, StatusExited, 1, true, false},
		{"always, exited zero", RestartAlways, StatusExited, 0, false, true},
		{"always, exited nonzero", RestartAlways, StatusExited, 1, false, true},
		{"always, exited but killed", RestartAlways, StatusExited, 0, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{
				status:           tt.status,
				exitCode:         tt.exitCode,
				killedExplicitly: tt.killedExplicitly,
				opts:             Options{Restart: tt.policy},
			}

			if got := sess.WillAutoRestart(); got != tt.want {
				t.Errorf("WillAutoRestart() = %v, want %v", got, tt.want)
			}
		})
	}
}

// newTestManagerWithRestart is newTestManager with backoff/success timers
// shrunk far enough that a real automatic restart can be observed inside a
// normal test deadline, without waiting on production-sized timers.
func newTestManagerWithRestart(t *testing.T) *Manager {
	t.Helper()

	m := newTestManager(t)
	m.RestartBackoffBase = 20 * time.Millisecond
	m.RestartBackoffMax = 200 * time.Millisecond
	m.RestartSuccessDuration = 150 * time.Millisecond

	return m
}

func waitForRestartAttempts(t *testing.T, id string, m *Manager, want int) *Session {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if sess, ok := m.Get(id); ok && sess.Status() == StatusRunning && sess.RestartAttempts() == want {
			return sess
		}

		time.Sleep(10 * time.Millisecond)
	}

	sess, _ := m.Get(id)
	t.Fatalf("timed out waiting for a running incarnation with RestartAttempts() = %d, got %+v", want, sess)

	return nil
}

func TestManagerAutoRestartsOnFailure(t *testing.T) {
	m := newTestManagerWithRestart(t)

	original, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Restart: RestartOnFailure})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := original.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	restarted := waitForRestartAttempts(t, original.ID, m, 1)

	if restarted.ID != original.ID {
		t.Errorf("restarted.ID = %s, want %s", restarted.ID, original.ID)
	}
	if restarted.Cmd == original.Cmd {
		t.Error("auto-restart reused the exited process instead of starting a new one")
	}
}

func TestManagerAlwaysPolicyRestartsOnZeroExit(t *testing.T) {
	m := newTestManagerWithRestart(t)

	original, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Restart: RestartAlways})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := original.Write([]byte("exit 0\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForRestartAttempts(t, original.ID, m, 1)
}

func TestManagerNeverPolicyDoesNotRestart(t *testing.T) {
	m := newTestManagerWithRestart(t)

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForStatus(t, sess, StatusExited)

	time.Sleep(500 * time.Millisecond) // long enough for a restart to have fired, if one were coming

	got, ok := m.Get(sess.ID)
	if !ok || got != sess || got.Status() != StatusExited {
		t.Errorf("session with restart: never was restarted; Get() = %+v", got)
	}
}

func TestManagerRestartAttemptsResetsAfterSuccessDuration(t *testing.T) {
	m := newTestManagerWithRestart(t)

	original, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Restart: RestartOnFailure})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := original.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	first := waitForRestartAttempts(t, original.ID, m, 1)

	// Let it stay up past RestartSuccessDuration without exiting again, then
	// fail once more: the next incarnation's attempt count must read 1, not
	// 2 — proof the counter actually reset rather than kept climbing.
	time.Sleep(m.RestartSuccessDuration + 100*time.Millisecond)

	if _, err := first.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForRestartAttempts(t, original.ID, m, 1)
}

// Killing a session that is sitting in a backoff wait (already exited, next
// attempt not yet fired) must suppress the pending restart for good — the
// same "an explicit stop always wins" rule as systemd's Restart=on-failure
// under `systemctl stop`.
func TestManagerKillDuringBackoffWaitSuppressesRestart(t *testing.T) {
	m := newTestManagerWithRestart(t)
	m.RestartBackoffBase = 300 * time.Millisecond
	m.RestartBackoffMax = 300 * time.Millisecond

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Restart: RestartOnFailure})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForStatus(t, sess, StatusExited)

	if err := m.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	time.Sleep(m.RestartBackoffBase + 200*time.Millisecond)

	got, ok := m.Get(sess.ID)
	if !ok || got != sess || got.Status() != StatusExited {
		t.Errorf("a killed session restarted anyway; Get() = %+v", got)
	}
}

// A manual restart while an automatic one is pending must cancel the
// pending one (no double restart) and hand the fresh incarnation a clean
// attempt count.
func TestManagerManualRestartDuringBackoffWaitCancelsPendingAutoRestartAndResetsAttempts(t *testing.T) {
	m := newTestManagerWithRestart(t)
	m.RestartBackoffBase = 300 * time.Millisecond
	m.RestartBackoffMax = 300 * time.Millisecond

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Restart: RestartOnFailure})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForStatus(t, sess, StatusExited)

	restarted, err := m.Restart(sess.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}

	if got := restarted.RestartAttempts(); got != 0 {
		t.Errorf("RestartAttempts() after a manual restart = %d, want 0", got)
	}

	// Give the (now cancelled) automatic restart's original window time to
	// have fired if cancellation had failed — it must not have swapped in a
	// second incarnation behind the manual one.
	time.Sleep(m.RestartBackoffBase + 200*time.Millisecond)

	got, ok := m.Get(sess.ID)
	if !ok || got != restarted {
		t.Errorf("a pending auto-restart fired after a manual restart already replaced it; Get() = %+v, want %+v", got, restarted)
	}
}

// Removing a session while it sits in a backoff wait must prevent the
// pending restart from resurrecting it under an id nothing references
// anymore.
func TestManagerRemoveDuringBackoffWaitPreventsRestart(t *testing.T) {
	m := newTestManagerWithRestart(t)
	m.RestartBackoffBase = 300 * time.Millisecond
	m.RestartBackoffMax = 300 * time.Millisecond

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Restart: RestartOnFailure})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForStatus(t, sess, StatusExited)

	if err := m.Remove(sess.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	time.Sleep(m.RestartBackoffBase + 200*time.Millisecond)

	if _, ok := m.Get(sess.ID); ok {
		t.Error("a removed session came back via a pending auto-restart")
	}
}
