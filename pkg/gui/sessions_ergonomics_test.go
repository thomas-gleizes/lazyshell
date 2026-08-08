package gui

import (
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
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

func TestNewNamedSessionSubmitUsesGivenName(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	before := len(gui.sessions.List())

	if err := gui.newNamedSession(gui.g, nil); err != nil {
		t.Fatalf("newNamedSession: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	if got := view.Buffer(); got != "" {
		t.Errorf("prompt pre-filled with %q, want an empty field", got)
	}

	view.Clear()
	if _, err := view.Write([]byte("build")); err != nil {
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
	if created.Name() != "build" {
		t.Errorf("created session Name() = %q, want %q", created.Name(), "build")
	}

	if selected := gui.selectedSession(); selected.ID != created.ID {
		t.Errorf("selected session = %s, want the new one %s", selected.ID, created.ID)
	}
}

// An empty answer is the documented fallback to the generated name — unlike
// newSessionInDir, where an empty directory means "never mind".
func TestNewNamedSessionEmptySubmitFallsBackToGeneratedName(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	before := len(gui.sessions.List())

	if err := gui.newNamedSession(gui.g, nil); err != nil {
		t.Fatalf("newNamedSession: %v", err)
	}

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()

	if err := gui.submitPrompt(gui.g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	sessions := gui.sessions.List()
	if len(sessions) != before+1 {
		t.Fatalf("session count after empty submit = %d, want %d", len(sessions), before+1)
	}

	if got := sessions[len(sessions)-1].Name(); got != "session-1" {
		t.Errorf("created session Name() = %q, want the generated %q", got, "session-1")
	}
}

// The generated-name counter is only spent when it is actually used, so a run
// of named sessions does not leave gaps in the "session-N" sequence.
func TestNewNamedSessionDoesNotConsumeCounter(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.createSession("build"); err != nil {
		t.Fatalf("createSession: %v", err)
	}

	if err := gui.newSession(gui.g, nil); err != nil {
		t.Fatalf("newSession: %v", err)
	}

	sessions := gui.sessions.List()
	if got := sessions[len(sessions)-1].Name(); got != "session-1" {
		t.Errorf("generated name = %q, want %q", got, "session-1")
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

// restartSession refuses a running session rather than silently no-op'ing, so
// the user learns why nothing happened.
func TestRestartSessionRefusesARunningSession(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.restartSession(gui.g, nil); err != nil {
		t.Fatalf("restartSession: %v", err)
	}

	if gui.lastError == "" {
		t.Error("restartSession on a running session left lastError empty")
	}
}

func TestRestartSessionRecreatesAnExitedSession(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)
	sess := gui.selectedSession()

	if err := gui.sessions.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session never terminated")
	}

	if err := gui.restartSession(gui.g, nil); err != nil {
		t.Fatalf("restartSession: %v", err)
	}

	if gui.lastError != "" {
		t.Errorf("lastError after a successful restart = %q, want empty", gui.lastError)
	}

	restarted := gui.selectedSession()
	if restarted.ID != sess.ID {
		t.Errorf("selected session after restart = %s, want the same id %s", restarted.ID, sess.ID)
	}
	if restarted.Status() != session.StatusRunning {
		t.Errorf("Status() after restart = %v, want %v", restarted.Status(), session.StatusRunning)
	}
}
