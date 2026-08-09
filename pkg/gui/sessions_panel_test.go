package gui

import (
	"fmt"
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
	got := sessionsPanelContent(nil, testMarkers, "", nil, nil, nil, 0)

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

	got := sessionsPanelContent([]*session.Session{a, b}, testMarkers, "", nil, nil, nil, 0)

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
		map[string]string{working.ID: "1.2k tokens"}, nil, 0)

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

// The bug this guards against: with a list longer than the panel, the
// selected line could be past what fits on screen, and nothing ever moved the
// view's origin to bring it into the visible window — the same lines stayed
// hidden below the frame no matter what was selected.
func TestApplySessionsPanelUpdateScrollsToKeepSelectionVisible(t *testing.T) {
	gui, g := newHeadlessGuiSized(t, 80, 20)

	const total = 30
	for i := range total {
		if _, err := gui.sessions.New(fmt.Sprintf("s%02d", i), "/bin/sh"); err != nil {
			t.Fatalf("sessions.New: %v", err)
		}
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := g.View(sessionsViewName)
	if err != nil {
		t.Fatalf("sessions view not found: %v", err)
	}

	innerHeight := view.InnerHeight()
	if innerHeight >= total {
		t.Fatalf("test terminal too tall to exercise overflow: InnerHeight=%d, want < %d", innerHeight, total)
	}

	content := sessionsPanelContent(gui.filteredSessions(), testMarkers, "", nil, nil, nil, 0)

	// The last session is well past what a 20-row terminal's panel shows.
	last := total - 1
	applySessionsPanelUpdate(view, content, last)

	origin := view.OriginY()
	if origin == 0 {
		t.Fatal("origin did not move: the selected line is past InnerHeight and would never be drawn")
	}
	if last < origin || last >= origin+innerHeight {
		t.Errorf("selected line %d is outside the visible window [%d, %d)", last, origin, origin+innerHeight)
	}

	// Selecting the first session again must scroll all the way back: a stale
	// offset here would hide it above the viewport instead, the same bug in
	// the other direction.
	applySessionsPanelUpdate(view, content, 0)

	if origin := view.OriginY(); origin != 0 {
		t.Errorf("origin = %d after selecting the first session, want 0", origin)
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
