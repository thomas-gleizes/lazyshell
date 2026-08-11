package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// The sessions list's restart indicator: a session that has needed at least
// one automatic restart gets "↻<count>" prepended onto its detail column.
func TestRestartIndicatorShowsAfterAutoRestart(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.sessions.RestartBackoffBase = 20 * time.Millisecond
	gui.sessions.RestartBackoffMax = 200 * time.Millisecond

	original, err := gui.sessions.NewWithOptions(session.Options{
		Name: "s0", Shell: "/bin/sh", Restart: session.RestartOnFailure,
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := original.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForCondition(t, func() bool {
		sess, ok := gui.sessions.Get(original.ID)

		return ok && sess.Status() == session.StatusRunning && sess.RestartAttempts() == 1
	}, "the session did not auto-restart")

	restarted, _ := gui.sessions.Get(original.ID)

	set := gui.markerSet()
	line := sessionLine(restarted, set, "", nil, nil)
	if !strings.Contains(line, colorizeMarker(restartMarker, "33")+"1") {
		t.Errorf("sessionLine = %q, want the restart indicator with count 1", line)
	}
}

// A session that has never needed a restart must not show the indicator —
// the overwhelming majority of sessions, which never declared restart: at
// all, must render exactly as before this feature existed.
func TestRestartIndicatorHiddenWithoutARestart(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "plain-session")

	set := gui.markerSet()
	line := sessionLine(sess, set, "", nil, nil)
	if strings.Contains(line, restartMarker) {
		t.Errorf("sessionLine = %q, indicator shown for a session with no restart", line)
	}
}

// The glyph is configurable like every other marker, and turning it off (the
// empty-string convention every marker shares) must suppress it even for a
// session that did need a restart.
func TestRestartIndicatorOffTurnsItOff(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.sessions.RestartBackoffBase = 20 * time.Millisecond
	gui.sessions.RestartBackoffMax = 200 * time.Millisecond

	original, err := gui.sessions.NewWithOptions(session.Options{
		Name: "s0", Shell: "/bin/sh", Restart: session.RestartOnFailure,
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := original.Write([]byte("exit 1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForCondition(t, func() bool {
		sess, ok := gui.sessions.Get(original.ID)

		return ok && sess.Status() == session.StatusRunning && sess.RestartAttempts() == 1
	}, "the session did not auto-restart")

	restarted, _ := gui.sessions.Get(original.ID)

	set := gui.markerSet()
	set.restart = ""

	line := sessionLine(restarted, set, "", nil, nil)
	if strings.Contains(line, "↻") {
		t.Errorf("sessionLine = %q, indicator shown despite an empty configured marker", line)
	}
}
