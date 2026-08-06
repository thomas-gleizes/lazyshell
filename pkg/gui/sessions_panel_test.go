package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

func TestSessionsPanelContentEmpty(t *testing.T) {
	got := sessionsPanelContent(nil)

	if !strings.Contains(got, "n pour en créer une") {
		t.Errorf("empty content = %q, want a hint about the n keybinding", got)
	}
}

func TestSessionsPanelContentListsSessions(t *testing.T) {
	m := session.NewManager()
	m.KillTimeout = 300 * time.Millisecond
	t.Cleanup(m.Shutdown)

	a, err := m.New("shell-a", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := m.New("shell-b", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := sessionsPanelContent([]*session.Session{a, b})

	for _, want := range []string{a.Name(), b.Name(), a.Cwd} {
		if !strings.Contains(got, want) {
			t.Errorf("content %q missing %q", got, want)
		}
	}

	if strings.Count(got, "\n") != 2 {
		t.Errorf("content = %q, want exactly 2 lines", got)
	}
}

// renderSessionsPanel runs on a ticker that starts before the first layout
// pass, so it must tolerate a missing view.
func TestRenderSessionsPanelWithoutView(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if err := gui.renderSessionsPanel(); err != nil {
		t.Fatalf("renderSessionsPanel: %v", err)
	}
}

func TestSelectionMovedClampsAtBounds(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	a, err := gui.sessions.New("a", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := gui.sessions.New("b", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	move := func(delta int) {
		t.Helper()
		if err := gui.selectionMoved(delta)(nil, nil); err != nil {
			t.Fatalf("selectionMoved(%d): %v", delta, err)
		}
	}

	// Starts at 0; moving down once should land on b.
	move(1)
	if got := gui.selectedSession(); got.ID != b.ID {
		t.Errorf("selected = %s, want %s", got.ID, b.ID)
	}

	// A second "down" must clamp at the last index, not run off the list.
	move(1)
	if got := gui.selectedSession(); got.ID != b.ID {
		t.Errorf("selected after clamping at the end = %s, want %s", got.ID, b.ID)
	}

	move(-1)
	if got := gui.selectedSession(); got.ID != a.ID {
		t.Errorf("selected = %s, want %s", got.ID, a.ID)
	}

	// Clamp at the start too.
	move(-1)
	if got := gui.selectedSession(); got.ID != a.ID {
		t.Errorf("selected after clamping at the start = %s, want %s", got.ID, a.ID)
	}
}

func TestSelectionMovedOnEmptyListIsNoop(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if err := gui.selectionMoved(1)(nil, nil); err != nil {
		t.Fatalf("selectionMoved on an empty list: %v", err)
	}

	if gui.selectedSession() != nil {
		t.Error("selectedSession() is non-nil despite an empty session list")
	}
}
