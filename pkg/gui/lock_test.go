package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
)

// newLockTestGui builds a Gui with two real sessions and the output view up,
// ready to move the selection between them. Returns the two sessions' ids in
// display order.
func newLockTestGui(t *testing.T) (*Gui, *gocui.Gui, string, string) {
	t.Helper()

	gui, g := newHeadlessGui(t)

	first, err := gui.sessions.New("first", "/bin/sh")
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	second, err := gui.sessions.New("second", "/bin/sh")
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	return gui, g, first.ID, second.ID
}

func selectSession(t *testing.T, gui *Gui, index int) {
	t.Helper()

	if err := gui.applySelection(index); err != nil {
		t.Fatalf("applySelection(%d): %v", index, err)
	}
}

// The project file's declared state (ADR 0012): selecting a session marked
// locked locks the panel, and going back to one marked unlocked arms
// pass-through again.
func TestSelectionAppliesDeclaredLockState(t *testing.T) {
	gui, _, first, second := newLockTestGui(t)

	gui.SetLockedSessions(map[string]bool{first: false, second: true})

	selectSession(t, gui, 1)

	if gui.passThroughActive {
		t.Error("selecting the session declared locked: passThroughActive = true, want locked")
	}

	selectSession(t, gui, 0)

	if !gui.passThroughActive {
		t.Error("selecting the session declared unlocked: passThroughActive = false, want pass-through")
	}
}

// A session nobody ever decided about keeps whatever the flag already said —
// ADR 0011's persistence, unchanged for everything undeclared.
func TestSelectionKeepsTheFlagForUndeclaredSessions(t *testing.T) {
	gui, _, _, _ := newLockTestGui(t)

	gui.lockOutput()
	selectSession(t, gui, 1)

	if gui.passThroughActive {
		t.Error("locked then moved to an undeclared session: want the lock carried over")
	}

	if len(gui.lockedBySession) != 0 {
		t.Errorf("lockedBySession = %v, want navigation alone to record nothing", gui.lockedBySession)
	}
}

// An explicit lock gesture is remembered, and only against the session it was
// made on: coming back finds it locked, its neighbour is untouched.
func TestExplicitLockIsRememberedPerSession(t *testing.T) {
	gui, _, first, second := newLockTestGui(t)

	selectSession(t, gui, 0)
	gui.exitPassThrough() // the prefix key / Esc-Esc gesture

	if got, ok := gui.lockedBySession[first]; !ok || !got {
		t.Fatalf("lockedBySession[first] = %v (present: %v), want true", got, ok)
	}

	if _, ok := gui.lockedBySession[second]; ok {
		t.Error("locking the first session recorded something for the second one")
	}

	// Unlocking the second session must not unlock the first.
	selectSession(t, gui, 1)
	gui.enterPassThrough()

	if !gui.passThroughActive {
		t.Fatal("enterPassThrough on the second session: still locked")
	}

	selectSession(t, gui, 0)

	if gui.passThroughActive {
		t.Error("back on the first session: want the remembered lock restored")
	}
}

// A round trip through a secondary tab must not pin the session to locked:
// setTab locks for a reason of its own, and lockOutput is what keeps that out
// of the remembered state.
func TestTabRoundTripDoesNotRememberTheLock(t *testing.T) {
	gui, _, first, _ := newLockTestGui(t)

	selectSession(t, gui, 0)

	if !gui.passThroughActive {
		t.Fatal("setup: want pass-through armed before leaving the terminal tab")
	}

	gui.setTab(tabResources)

	if gui.passThroughActive {
		t.Fatal("leaving the terminal tab: want the panel locked")
	}

	if _, ok := gui.lockedBySession[first]; ok {
		t.Errorf("lockedBySession = %v, want a tab switch to record nothing", gui.lockedBySession)
	}

	// Back on the terminal tab, the session is still undecided, so selecting
	// it again must not resurrect a lock nobody asked for.
	gui.setTab(tabTerminal)
	gui.enterPassThrough()
	selectSession(t, gui, 0)

	if !gui.passThroughActive {
		t.Error("after a tab round trip: want pass-through, not a remembered lock")
	}
}

// Restoring a remembered pass-through must not fire while a secondary tab is
// showing: editOutput tests the flag before the tab, so that would type into a
// static report the user cannot see.
func TestRememberedPassThroughIsNotRestoredOnASecondaryTab(t *testing.T) {
	gui, _, first, _ := newLockTestGui(t)

	gui.SetLockedSessions(map[string]bool{first: false})
	gui.setTab(tabResources)

	selectSession(t, gui, 0)

	if gui.passThroughActive {
		t.Error("pass-through armed while the resources tab is showing")
	}

	// Back on the terminal tab, the same selection restores it.
	gui.setTab(tabTerminal)
	selectSession(t, gui, 0)

	if !gui.passThroughActive {
		t.Error("back on the terminal tab: want the remembered pass-through restored")
	}
}

// Deleting a session drops its remembered state, alongside its perf history.
func TestForgetLockStateOnDelete(t *testing.T) {
	gui, _, first, second := newLockTestGui(t)

	gui.SetLockedSessions(map[string]bool{first: true, second: true})
	gui.forgetLockState(first)

	if _, ok := gui.lockedBySession[first]; ok {
		t.Error("forgetLockState kept the entry")
	}

	if _, ok := gui.lockedBySession[second]; !ok {
		t.Error("forgetLockState dropped the wrong entry")
	}
}
