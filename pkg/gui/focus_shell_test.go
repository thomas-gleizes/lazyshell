package gui

import (
	"testing"
)

// assertInsideShell checks the state focusSelectedShell is supposed to leave
// behind: the output panel focused, pass-through armed, keystrokes going to
// the shell.
func assertInsideShell(t *testing.T, gui *Gui, what string) {
	t.Helper()

	if !gui.passThroughActive {
		t.Errorf("%s did not arm pass-through", what)
	}

	current := gui.g.CurrentView()
	if current == nil || current.Name() != outputViewName {
		t.Errorf("current view after %s = %v, want the output panel", what, current)
	}
}

// Creating a session is only ever followed by typing into it, so "n" lands
// straight inside the new shell instead of leaving the user on the panel with
// Tab and "i" left to press.
func TestNewSessionLandsInsideTheNewShell(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.newSession(gui.g, nil); err != nil {
		t.Fatalf("newSession: %v", err)
	}

	assertInsideShell(t, gui, "newSession")

	sessions := gui.sessions.List()
	if selected := gui.selectedSession(); selected == nil || selected.ID != sessions[len(sessions)-1].ID {
		t.Error("newSession did not select the session it just created")
	}
}

func TestDuplicateSessionLandsInsideTheNewShell(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.duplicateSession(gui.g, nil); err != nil {
		t.Fatalf("duplicateSession: %v", err)
	}

	assertInsideShell(t, gui, "duplicateSession")
}

// The prompt path too: closePrompt restores focus to whatever was current
// before the popup, and the creation that follows it has to win over that.
func TestNewSessionInDirLandsInsideTheNewShell(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.newSessionInDir(gui.g, nil); err != nil {
		t.Fatalf("newSessionInDir: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}

	view.Clear()
	if _, err := view.Write([]byte(t.TempDir())); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := gui.submitPrompt(gui.g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	assertInsideShell(t, gui, "newSessionInDir")
}

// A restart is a fresh shell like any other creation — and it is the key the
// user reaches for right after an exit dropped them back on the panel.
func TestRestartSessionLandsInsideTheRestartedShell(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	sess := gui.selectedSession()
	if err := gui.sessions.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if err := gui.restartSession(gui.g, nil); err != nil {
		t.Fatalf("restartSession: %v", err)
	}

	if gui.lastError != "" {
		t.Fatalf("restartSession reported %q", gui.lastError)
	}

	assertInsideShell(t, gui, "restartSession")
}

// Moving the selection is navigation, not a creation: j/k must never arm
// pass-through under the user, or scrolling through the list would start
// typing into shells.
func TestMovingTheSelectionDoesNotArmPassThrough(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if _, err := gui.sessions.New("second", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	if _, err := gui.g.SetCurrentView(sessionsViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	if err := gui.selectionMoved(1)(gui.g, nil); err != nil {
		t.Fatalf("selectionMoved: %v", err)
	}

	if gui.passThroughActive {
		t.Error("moving the selection armed pass-through")
	}

	if current := gui.g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view after a selection move = %v, want the sessions panel", current)
	}
}

// A creation that fails must not hand the keyboard to a shell that does not
// exist: newSession reports the error and stays where it is.
func TestFailedNewSessionStaysOnThePanel(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if _, err := gui.g.SetCurrentView(sessionsViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	gui.configuredShell = "/nonexistent/shell-that-cannot-start"

	if err := gui.newSession(gui.g, nil); err != nil {
		t.Fatalf("newSession: %v", err)
	}

	if gui.lastError == "" {
		t.Error("a shell that cannot start was not reported")
	}

	if gui.passThroughActive {
		t.Error("a failed creation armed pass-through")
	}

	if current := gui.g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view after a failed creation = %v, want the sessions panel", current)
	}
}
