package app

import (
	"path/filepath"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// writeConfig points LAZYSHELL_CONFIG at a fresh file with content, so a test
// can set a user-config field (here, restore_layout) without touching the
// real ~/.config/lazyshell.
func writeConfig(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yml")
	writeFile(t, path, content)
	t.Setenv("LAZYSHELL_CONFIG", path)
}

func TestSavedLayoutIsIgnoredWhenNever(t *testing.T) {
	dir := projectDir(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.SaveState(dir, []config.StateSession{{Name: "api", Cwd: dir}}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	writeConfig(t, "restore_layout: never\n")

	a := newTestApp(t, Options{}, nonInteractive)

	sessions := a.sessions.List()
	if len(sessions) != 1 || sessions[0].Name() != "session-1" {
		t.Fatalf("sessions = %v, want the single default session (restore_layout: never)", sessions)
	}
}

func TestSavedLayoutIsRestoredAutomaticallyWhenAlways(t *testing.T) {
	dir := projectDir(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.SaveState(dir, []config.StateSession{
		{Name: "api", Cwd: dir, Command: "echo restored"},
		{Name: "logs", Cwd: dir},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	writeConfig(t, "restore_layout: always\n")

	a := newTestApp(t, Options{}, nonInteractive)

	sessions := a.sessions.List()
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2 (the saved layout)", len(sessions))
	}
	if sessions[0].Name() != "api" || sessions[1].Name() != "logs" {
		t.Errorf("sessions = %q/%q, want api/logs in saved order", sessions[0].Name(), sessions[1].Name())
	}
}

// The default: nothing starts synchronously, and the saved layout is handed
// to the GUI so Run can queue the confirmation popup once the terminal is up
// — see docs/adr/0013-persistance-de-la-disposition.md.
func TestSavedLayoutIsHandedToGuiForConfirmationWhenAsk(t *testing.T) {
	dir := projectDir(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.SaveState(dir, []config.StateSession{{Name: "api", Cwd: dir}}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	// No config.yml written: "ask" is the default.

	a := newTestApp(t, Options{}, nonInteractive)

	if got := len(a.sessions.List()); got != 0 {
		t.Errorf("len(sessions) = %d, want 0 (nothing started before the popup is answered)", got)
	}

	state, _ := a.gui.PendingRestore()
	if state == nil || len(state.Sessions) != 1 || state.Sessions[0].Name != "api" {
		t.Errorf("PendingRestore = %+v, want the saved layout handed to the GUI", state)
	}
}

// A lazyshell.yml always wins: the saved layout is never even read, per
// decision 1 of ADR 0013.
func TestProjectFileWinsOverSavedLayout(t *testing.T) {
	dir := projectDir(t, "sessions:\n  - name: declared\n")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.SaveState(dir, []config.StateSession{{Name: "from-state", Cwd: dir}}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	a := newTestApp(t, Options{}, answering("y"))

	sessions := a.sessions.List()
	if len(sessions) != 1 || sessions[0].Name() != "declared" {
		t.Fatalf("sessions = %v, want only the project file's declared session", sessions)
	}

	state, _ := a.gui.PendingRestore()
	if state != nil {
		t.Errorf("PendingRestore = %+v, want nil (never read when a project file is present)", state)
	}
}

// End-to-end: App.Run's snapshot defer actually writes what the manager was
// holding, in a shape LoadState can read back.
func TestSnapshotStateWritesWhatWasRunning(t *testing.T) {
	dir := projectDir(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	a := newTestApp(t, Options{}, nonInteractive)

	sessions := a.sessions.List()
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1 (default session)", len(sessions))
	}

	a.snapshotState()

	state, err := config.LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state == nil || len(state.Sessions) != 1 || state.Sessions[0].Name != sessions[0].Name() {
		t.Errorf("LoadState = %+v, want one session named %q", state, sessions[0].Name())
	}
}
