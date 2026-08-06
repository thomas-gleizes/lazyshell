package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The "arrêt global" requirement: every session must die, and Shutdown must
// not return until they all have, so a caller can safely exit the process
// right after.
func TestManagerShutdownKillsAllSessions(t *testing.T) {
	m := newTestManager(t)

	sessions := []*Session{
		newTestSession(t, m, "a"),
		newTestSession(t, m, "b"),
		newTestSession(t, m, "c"),
	}

	for _, sess := range sessions {
		if _, err := sess.Write([]byte("sleep 30\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	time.Sleep(300 * time.Millisecond) // let every shell actually start sleep

	m.Shutdown()

	for _, sess := range sessions {
		if sess.Status() != StatusExited {
			t.Errorf("session %s: Status() = %v, want %v", sess.ID, sess.Status(), StatusExited)
		}
	}
}

// NewInDir is the "session dans un cwd choisi" ergonomics feature: it must
// start the shell in the given directory rather than the process's own cwd.
func TestManagerNewInDirUsesGivenCwd(t *testing.T) {
	m := newTestManager(t)

	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	sess, err := m.NewInDir("a", testShell, dir)
	if err != nil {
		t.Fatalf("NewInDir: %v", err)
	}

	if sess.Cwd != dir {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, dir)
	}
	if sess.Cmd.Dir != dir {
		t.Errorf("Cmd.Dir = %q, want %q", sess.Cmd.Dir, dir)
	}

	if _, err := sess.Write([]byte("pwd\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForScreen(t, sess, resolved)
}

// New is documented as a thin wrapper around NewInDir using the process's own
// working directory — this pins that behaviour down.
func TestManagerNewUsesProcessCwd(t *testing.T) {
	m := newTestManager(t)

	wantCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	sess := newTestSession(t, m, "a")

	if sess.Cwd != wantCwd {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, wantCwd)
	}
}

// ScrollbackSize, when set on the Manager (pkg/config's ScrollbackSize),
// must reach the session's terminal emulator rather than the emulator's own
// built-in default.
func TestManagerScrollbackSizeIsAppliedToNewSessions(t *testing.T) {
	m := newTestManager(t)
	m.ScrollbackSize = 3

	sess := newTestSession(t, m, "a")

	// Force enough lines through the emulator to overflow a 3-line scrollback
	// many times over, then check it never grew past the configured cap.
	for range 20 {
		if _, err := sess.Write([]byte("echo line\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && sess.Screen().ScrollbackLen() < 3 {
		time.Sleep(20 * time.Millisecond)
	}

	if got := sess.Screen().ScrollbackLen(); got > 3 {
		t.Errorf("ScrollbackLen() = %d, want <= 3 (configured ScrollbackSize)", got)
	}
}

func TestManagerGetUnknownIDReturnsFalse(t *testing.T) {
	m := newTestManager(t)

	if _, ok := m.Get("no-such-id"); ok {
		t.Error("Get on an unknown id returned ok = true")
	}
}

func TestManagerListPreservesInsertionOrder(t *testing.T) {
	m := newTestManager(t)

	a := newTestSession(t, m, "a")
	b := newTestSession(t, m, "b")
	c := newTestSession(t, m, "c")

	got := m.List()
	if len(got) != 3 {
		t.Fatalf("List() has %d sessions, want 3", len(got))
	}

	want := []*Session{a, b, c}
	for i, sess := range want {
		if got[i].ID != sess.ID {
			t.Errorf("List()[%d].ID = %s, want %s", i, got[i].ID, sess.ID)
		}
	}
}

// Restart must bring back an exited session with the same id, name and cwd —
// same position in List() too, since the id does not change.
func TestManagerRestartRecreatesAnExitedSession(t *testing.T) {
	m := newTestManager(t)

	dir := t.TempDir()
	original, err := m.NewWithOptions(Options{Name: "build", Shell: testShell, Cwd: dir})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := original.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForStatus(t, original, StatusExited)

	restarted, err := m.Restart(original.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}

	if restarted.ID != original.ID {
		t.Errorf("Restart() ID = %s, want the same id %s", restarted.ID, original.ID)
	}
	if restarted.Name() != "build" {
		t.Errorf("Restart() Name() = %q, want %q", restarted.Name(), "build")
	}
	if restarted.Cwd != dir {
		t.Errorf("Restart() Cwd = %q, want %q", restarted.Cwd, dir)
	}
	if restarted.Status() != StatusRunning {
		t.Errorf("Restart() Status() = %v, want %v", restarted.Status(), StatusRunning)
	}
	if restarted.Cmd == original.Cmd {
		t.Error("Restart() reused the exited process instead of starting a new one")
	}

	got, ok := m.Get(original.ID)
	if !ok || got != restarted {
		t.Error("Manager.sessions was not swapped to the restarted session")
	}

	list := m.List()
	if len(list) != 1 || list[0].ID != original.ID {
		t.Errorf("List() = %v, want the single restarted session in the same slot", list)
	}
}

// A session still running must not be restarted out from under itself.
func TestManagerRestartRefusesARunningSession(t *testing.T) {
	m := newTestManager(t)

	sess := newTestSession(t, m, "s")

	if _, err := m.Restart(sess.ID); err == nil {
		t.Error("Restart on a running session did not error")
	}
}

func TestManagerRestartUnknownIDReturnsError(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.Restart("no-such-id"); err == nil {
		t.Error("Restart on an unknown id did not error")
	}
}
