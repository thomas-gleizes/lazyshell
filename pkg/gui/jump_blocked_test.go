package gui

import (
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

func TestJumpToNextBlockedSessionFindsTheOnlyOne(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	newTestSession(t, gui, "a")
	b := newTestSession(t, gui, "b")
	newTestSession(t, gui, "c")

	b.SetAgentState(agent.StateBlocked)

	if err := gui.jumpToNextBlockedSession(nil, nil); err != nil {
		t.Fatalf("jumpToNextBlockedSession: %v", err)
	}

	if got := gui.selectedSession(); got == nil || got.ID != b.ID {
		t.Errorf("selected = %v, want %s", got, b.ID)
	}
}

func TestJumpToNextBlockedSessionCyclesFromCurrentSelection(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	a := newTestSession(t, gui, "a")
	newTestSession(t, gui, "b")
	c := newTestSession(t, gui, "c")

	a.SetAgentState(agent.StateBlocked)
	c.SetAgentState(agent.StateBlocked)

	// Selection starts at index 0 (a, already blocked) — jumping must find
	// the *next* one, not stay put on the currently selected match.
	if err := gui.jumpToNextBlockedSession(nil, nil); err != nil {
		t.Fatalf("jumpToNextBlockedSession: %v", err)
	}

	if got := gui.selectedSession(); got == nil || got.ID != c.ID {
		t.Errorf("selected = %v, want %s (c, the next blocked session after a)", got, c.ID)
	}

	// From c, wrapping around must land back on a.
	if err := gui.jumpToNextBlockedSession(nil, nil); err != nil {
		t.Fatalf("jumpToNextBlockedSession: %v", err)
	}

	if got := gui.selectedSession(); got == nil || got.ID != a.ID {
		t.Errorf("selected after wrapping = %v, want %s", got, a.ID)
	}
}

func TestJumpToNextBlockedSessionNoopWhenNoneBlocked(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	a := newTestSession(t, gui, "a")
	newTestSession(t, gui, "b")

	if err := gui.jumpToNextBlockedSession(nil, nil); err != nil {
		t.Fatalf("jumpToNextBlockedSession: %v", err)
	}

	if got := gui.selectedSession(); got == nil || got.ID != a.ID {
		t.Errorf("selection moved despite no blocked session: got %v, want unchanged %s", got, a.ID)
	}
}

func TestJumpToNextBlockedSessionOnEmptyListIsNoop(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if err := gui.jumpToNextBlockedSession(nil, nil); err != nil {
		t.Fatalf("jumpToNextBlockedSession on an empty list: %v", err)
	}

	if gui.selectedSession() != nil {
		t.Error("selectedSession() is non-nil despite an empty session list")
	}
}
