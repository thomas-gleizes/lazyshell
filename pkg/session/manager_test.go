package session

import (
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
