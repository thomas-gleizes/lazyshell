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
