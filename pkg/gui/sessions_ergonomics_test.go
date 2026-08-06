package gui

import (
	"testing"
)

// newSessionsErgonomicsTestGui builds a Gui with one real session selected
// and every view already created, ready to drive rename/duplicate/new-in-dir
// directly — same convention as newOutputTestGui (input_test.go).
func newSessionsErgonomicsTestGui(t *testing.T) *Gui {
	t.Helper()

	gui, g := newHeadlessGui(t)

	if _, err := gui.sessions.New("original", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	gui.onSelectionChanged()

	return gui
}

func TestRenameSessionOpensPromptPreFilledWithCurrentName(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.renameSession(gui.g, nil); err != nil {
		t.Fatalf("renameSession: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	if got := view.Buffer(); got != "original" {
		t.Errorf("prompt pre-filled with %q, want %q", got, "original")
	}
}

func TestRenameSessionSubmitRenamesSelectedSession(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	sess := gui.selectedSession()

	if err := gui.renameSession(gui.g, nil); err != nil {
		t.Fatalf("renameSession: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()
	if _, err := view.Write([]byte("renamed")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := gui.submitPrompt(gui.g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	if got := sess.Name(); got != "renamed" {
		t.Errorf("Name() after rename = %q, want %q", got, "renamed")
	}
}

func TestRenameSessionEmptySubmitLeavesNameUnchanged(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	sess := gui.selectedSession()

	if err := gui.renameSession(gui.g, nil); err != nil {
		t.Fatalf("renameSession: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()

	if err := gui.submitPrompt(gui.g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	if got := sess.Name(); got != "original" {
		t.Errorf("Name() after empty submit = %q, want unchanged %q", got, "original")
	}
}

func TestDuplicateSessionStartsANewSessionInTheSameDir(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	before := len(gui.sessions.List())
	original := gui.selectedSession()

	if err := gui.duplicateSession(gui.g, nil); err != nil {
		t.Fatalf("duplicateSession: %v", err)
	}

	sessions := gui.sessions.List()
	if len(sessions) != before+1 {
		t.Fatalf("session count = %d, want %d", len(sessions), before+1)
	}

	dup := sessions[len(sessions)-1]
	if dup.Cwd != original.Cwd {
		t.Errorf("duplicate Cwd = %q, want %q", dup.Cwd, original.Cwd)
	}
	if dup.Name() != original.Name()+"-copie" {
		t.Errorf("duplicate Name() = %q, want %q", dup.Name(), original.Name()+"-copie")
	}

	// duplicateSession selects the new session, matching newSession's own
	// behaviour.
	if selected := gui.selectedSession(); selected.ID != dup.ID {
		t.Errorf("selected session after duplicate = %s, want the duplicate %s", selected.ID, dup.ID)
	}
}

func TestNewSessionInDirOpensPromptPreFilledWithCwd(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.newSessionInDir(gui.g, nil); err != nil {
		t.Fatalf("newSessionInDir: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	if got := view.Buffer(); got == "" {
		t.Error("prompt not pre-filled with a directory")
	}
}

func TestNewSessionInDirSubmitStartsSessionInGivenDir(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	before := len(gui.sessions.List())
	dir := t.TempDir()

	if err := gui.newSessionInDir(gui.g, nil); err != nil {
		t.Fatalf("newSessionInDir: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()
	if _, err := view.Write([]byte(dir)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := gui.submitPrompt(gui.g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	sessions := gui.sessions.List()
	if len(sessions) != before+1 {
		t.Fatalf("session count = %d, want %d", len(sessions), before+1)
	}

	created := sessions[len(sessions)-1]
	if created.Cwd != dir {
		t.Errorf("created session Cwd = %q, want %q", created.Cwd, dir)
	}
}

func TestNewSessionInDirEmptySubmitCreatesNoSession(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	before := len(gui.sessions.List())

	if err := gui.newSessionInDir(gui.g, nil); err != nil {
		t.Fatalf("newSessionInDir: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()

	if err := gui.submitPrompt(gui.g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	if got := len(gui.sessions.List()); got != before {
		t.Errorf("session count after empty submit = %d, want unchanged %d", got, before)
	}
}
