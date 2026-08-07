package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// testMarkers is the built-in gutter, the same pair a Gui with no configured
// markers resolves to. Used by every test that calls sessionsPanelContent
// directly instead of going through a Gui.
var testMarkers = markerSet{bell: bellMarker, altScreen: altScreenMarker, activity: activityMarker}

func TestSessionsPanelContentEmpty(t *testing.T) {
	got := sessionsPanelContent(nil, testMarkers, "", nil, nil, nil)

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

	got := sessionsPanelContent([]*session.Session{a, b}, testMarkers, "", nil, nil, nil)

	for _, want := range []string{a.Name(), b.Name(), a.Cwd} {
		if !strings.Contains(got, want) {
			t.Errorf("content %q missing %q", got, want)
		}
	}

	if strings.Count(got, "\n") != 2 {
		t.Errorf("content = %q, want exactly 2 lines", got)
	}
}

// A session mid-turn shows its duration in the detail column; a stats line
// cached for it is appended after the duration. Neither appears for a
// session that has never entered StateWorking.
func TestSessionsPanelContentShowsTurnDurationAndStats(t *testing.T) {
	m := session.NewManager()
	m.KillTimeout = 300 * time.Millisecond
	t.Cleanup(m.Shutdown)

	quiet, err := m.New("quiet", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	working, err := m.New("working", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	working.SetAgentState(agent.StateWorking)

	content := sessionsPanelContent([]*session.Session{quiet, working}, testMarkers, "", nil,
		map[string]string{working.ID: "1.2k tokens"}, nil)

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per session:\n%q", len(lines), lines)
	}

	if strings.Contains(lines[0], "⏱") {
		t.Errorf("quiet session shows a turn duration: %q", lines[0])
	}

	if !strings.Contains(lines[1], "⏱") {
		t.Errorf("working session has no turn duration: %q", lines[1])
	}

	if !strings.Contains(lines[1], "1.2k tokens") {
		t.Errorf("working session has no stats line: %q", lines[1])
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

func TestSelectIndexJumpsDirectly(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	a, err := gui.sessions.New("a", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gui.sessions.New("b", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	c, err := gui.sessions.New("c", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.selectIndex(2)(nil, nil); err != nil {
		t.Fatalf("selectIndex(2): %v", err)
	}
	if got := gui.selectedSession(); got.ID != c.ID {
		t.Errorf("selected = %s, want %s", got.ID, c.ID)
	}

	if err := gui.selectIndex(0)(nil, nil); err != nil {
		t.Fatalf("selectIndex(0): %v", err)
	}
	if got := gui.selectedSession(); got.ID != a.ID {
		t.Errorf("selected = %s, want %s", got.ID, a.ID)
	}

	// Pressing "9" (index 8) with only 3 sessions open must do nothing,
	// silently — not clamp to the last one, not error.
	if err := gui.selectIndex(8)(nil, nil); err != nil {
		t.Fatalf("selectIndex(8): %v", err)
	}
	if got := gui.selectedSession(); got.ID != a.ID {
		t.Errorf("selected after out-of-range jump = %s, want unchanged %s", got.ID, a.ID)
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
