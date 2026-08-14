package gui

import (
	"strings"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

func TestConfirmRestoreLayoutShowsPopupNamingSessions(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.SetPendingRestore(&config.StateFile{Sessions: []config.StateSession{
		{Name: "api", Cwd: "/tmp"},
		{Name: "logs", Cwd: "/tmp"},
	}}, "/bin/sh")

	if err := gui.confirmRestoreLayout(); err != nil {
		t.Fatalf("confirmRestoreLayout: %v", err)
	}

	view, err := g.View(confirmViewName)
	if err != nil {
		t.Fatalf("confirm popup not shown: %v", err)
	}
	if !strings.Contains(view.Buffer(), "api") || !strings.Contains(view.Buffer(), "logs") {
		t.Errorf("confirm popup = %q, want it to name both saved sessions", view.Buffer())
	}
	if !strings.Contains(view.Buffer(), "2") {
		t.Errorf("confirm popup = %q, want the session count", view.Buffer())
	}
}

// applyRestoredLayout is confirmRestoreLayout's onConfirm, exercised directly
// like acceptConfirm's own callers do — see quit_test.go.
func TestApplyRestoredLayoutCreatesEverySession(t *testing.T) {
	gui, g := newHeadlessGui(t)
	t.Cleanup(gui.sessions.Shutdown)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	state := &config.StateFile{Sessions: []config.StateSession{
		{Name: "api", Group: "backend", Cwd: "/tmp"},
		{Name: "logs", Cwd: "/tmp"},
	}}

	if err := gui.applyRestoredLayout(state); err != nil {
		t.Fatalf("applyRestoredLayout: %v", err)
	}

	sessions := gui.sessions.List()
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	if sessions[0].Name() != "api" || sessions[0].Group() != "backend" {
		t.Errorf("sessions[0] = %q/%q, want api/backend", sessions[0].Name(), sessions[0].Group())
	}
	if sessions[1].Name() != "logs" {
		t.Errorf("sessions[1] = %q, want logs", sessions[1].Name())
	}
}

// Declining leaves nothing behind: no fallback session is created — the
// popup's 'n'/Esc path is showConfirm's generic closeConfirm, exercised
// already by confirm_test.go/quit_test.go, so this only guards that nothing
// here provides a fallback to skip.
func TestDecliningRestoreCreatesNoSession(t *testing.T) {
	gui, g := newHeadlessGui(t)
	t.Cleanup(gui.sessions.Shutdown)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.SetPendingRestore(&config.StateFile{Sessions: []config.StateSession{{Name: "api", Cwd: "/tmp"}}}, "/bin/sh")

	if err := gui.confirmRestoreLayout(); err != nil {
		t.Fatalf("confirmRestoreLayout: %v", err)
	}

	if err := gui.closeConfirm(); err != nil {
		t.Fatalf("closeConfirm: %v", err)
	}

	if got := len(gui.sessions.List()); got != 0 {
		t.Errorf("len(sessions) = %d, want 0 (declined)", got)
	}
}
