package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// waitForWatchHit polls LastWatchHit rather than sleeping a fixed amount: the
// hit only lands once the real shell's output has actually round-tripped
// through the pty and the drain goroutine's feedWatch tap.
func waitForWatchHit(t *testing.T, sess *session.Session) session.WatchHit {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if hit, ok := sess.LastWatchHit(); ok {
			return hit
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for a watch hit")

	return session.WatchHit{}
}

// A notify-eligible pattern match fires exactly one notification, never a
// repeat for the same hit — same shape as
// TestCheckCommandExitNotificationsFiresOnceForNonAgentFailure.
func TestCheckWatchNotificationsFiresOnceForMatch(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	if _, err := sess.Write([]byte("echo 'ERR! oops'\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForWatchHit(t, sess)

	read := redirectStdout(t)
	if err := gui.checkWatchNotifications(); err != nil {
		t.Fatalf("checkWatchNotifications: %v", err)
	}
	if got := read(); !strings.Contains(got, "\x1b]9;") {
		t.Fatalf("no notification fired for a matched watch pattern: %q", got)
	}

	// Same hit again (seq unchanged): must not re-fire.
	read = redirectStdout(t)
	if err := gui.checkWatchNotifications(); err != nil {
		t.Fatalf("checkWatchNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification re-fired for the same watch hit: %q", got)
	}
}

// A watcher declared with notify: false must never notify — it exists for a
// future non-notification effect, not as a silent bug in this one.
func TestCheckWatchNotificationsSkipsNotifyFalse(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess, err := gui.sessions.NewWithOptions(session.Options{
		Name:  "s",
		Shell: "/bin/sh",
		Watch: []session.WatchSpec{{Pattern: "silent", Notify: false}},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo 'silent match'\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForWatchHit(t, sess)

	read := redirectStdout(t)
	if err := gui.checkWatchNotifications(); err != nil {
		t.Fatalf("checkWatchNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification fired for a watch declared with notify: false: %q", got)
	}
}
