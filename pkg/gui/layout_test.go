package gui

import (
	"testing"

	"github.com/jesseduffield/lazycore/pkg/boxlayout"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

func TestLayoutCreatesSessionsOutputAndStatusViews(t *testing.T) {
	gui, g := newHeadlessGui(t)

	// Called twice: the first call creates the views, the second one only
	// resizes them. gocui reports creation through a wrapped ErrUnknownView,
	// easy to mishandle.
	for i := range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout call %d: %v", i+1, err)
		}
	}

	for _, name := range []string{sessionsViewName, outputViewName, statusViewName} {
		if _, err := g.View(name); err != nil {
			t.Errorf("view %q not found: %v", name, err)
		}
	}

	if current := g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view = %v, want %q", current, sessionsViewName)
	}
}

// The bug this guards against: propagateResize used to size every session's
// pty and terminal emulator to the output view's Size(), which counts its two
// frame border rows. The shell then believed it had two more rows to draw
// into than the panel could actually show, so the bottom of anything that
// used the full height — a multi-line prompt, vim, htop — was silently
// clipped by the frame: on screen it looked exactly like content "hidden by
// the interface", indistinguishable from a rendering bug.
func TestPropagateResizeUsesInnerHeightNotFrameHeight(t *testing.T) {
	gui, g := newHeadlessGuiSized(t, 80, 24)

	sess, err := gui.sessions.New("s", "/bin/sh")
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	_, innerHeight := view.InnerSize()
	_, frameHeight := view.Size()
	if innerHeight == frameHeight {
		t.Fatalf("test terminal does not exercise the bug: InnerSize equals Size (%d)", innerHeight)
	}

	gotCols, gotRows := sess.Screen().Size()
	if gotRows != innerHeight {
		t.Errorf("session sized to %d rows, want %d (the view's InnerHeight — Size() would give %d, two rows too many for the panel's own frame)",
			gotRows, innerHeight, frameHeight)
	}

	if innerWidth, _ := view.InnerSize(); gotCols != innerWidth {
		t.Errorf("session sized to %d columns, want %d", gotCols, innerWidth)
	}
}

// A terminal too small to hold a bordered view must not make the layout fail.
func TestLayoutTinyTerminal(t *testing.T) {
	gui, g := newHeadlessGuiSized(t, 1, 1)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}
}

// The exit criterion in ROADMAP.md for the boxlayout tree: the split flips
// from side-by-side (landscape) to stacked (portrait) at the documented
// threshold, and the sessions panel's static size means a different axis in
// each case (width in landscape, height in portrait).
func TestRootBoxSwitchesToPortraitBelowThreshold(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	root := gui.rootBox()

	landscape := boxlayout.ArrangeWindows(root, 0, 0, 200, 50)
	sessions, ok := landscape[sessionsViewName]
	if !ok {
		t.Fatal("sessions view missing in landscape layout")
	}
	if width := sessions.X1 - sessions.X0 + 1; width != sessionsWidthLandscape {
		t.Errorf("landscape sessions width = %d, want %d", width, sessionsWidthLandscape)
	}
	output := landscape[outputViewName]
	if output.X0 <= sessions.X1 {
		t.Errorf("output does not start after sessions in landscape: sessions=%v output=%v", sessions, output)
	}

	portrait := boxlayout.ArrangeWindows(root, 0, 0, 80, 50)
	sessions, ok = portrait[sessionsViewName]
	if !ok {
		t.Fatal("sessions view missing in portrait layout")
	}
	if height := sessions.Y1 - sessions.Y0 + 1; height != sessionsHeightPortrait {
		t.Errorf("portrait sessions height = %d, want %d", height, sessionsHeightPortrait)
	}
	output = portrait[outputViewName]
	if output.Y0 <= sessions.Y1 {
		t.Errorf("output is not stacked below sessions in portrait: sessions=%v output=%v", sessions, output)
	}
}

// pkg/config's SessionsPanelWidth must reach rootBox's landscape sizing.
func TestRootBoxUsesConfiguredSessionsPanelWidth(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.sessionsPanelWidth = 50

	dimensions := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 200, 50)
	sessions, ok := dimensions[sessionsViewName]
	if !ok {
		t.Fatal("sessions view missing in landscape layout")
	}

	if width := sessions.X1 - sessions.X0 + 1; width != 50 {
		t.Errorf("landscape sessions width = %d, want configured 50", width)
	}
}

// Zoom is a flag on the same box, not a second layout tree: it must simply
// leave the sessions panel out of the arranged boxes, giving output the
// whole width.
func TestRootBoxZoomedHidesSessionsPanel(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.zoomed = true

	dimensions := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 200, 50)

	if _, ok := dimensions[sessionsViewName]; ok {
		t.Error("sessions view is still arranged while zoomed")
	}

	output, ok := dimensions[outputViewName]
	if !ok {
		t.Fatal("output view missing while zoomed")
	}
	if width := output.X1 - output.X0 + 1; width != 200 {
		t.Errorf("zoomed output width = %d, want the full 200", width)
	}
}

// The agents dashboard only shows up beside the sessions panel when at least
// one session has a detected agent — see pkg/gui/agents_panel.go.
func TestRootBoxHidesAgentsPanelWithNoDetectedAgent(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	newTestSession(t, gui, "a")

	dimensions := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 200, 50)

	if _, ok := dimensions[agentsViewName]; ok {
		t.Error("agents view is arranged despite no detected agent")
	}
}

func TestRootBoxShowsAgentsPanelBesideSessionsInLandscape(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "a")
	sess.SetAgentState(agent.StateWorking)

	dimensions := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 200, 50)

	sessions, ok := dimensions[sessionsViewName]
	if !ok {
		t.Fatal("sessions view missing in landscape layout")
	}

	agents, ok := dimensions[agentsViewName]
	if !ok {
		t.Fatal("agents view missing despite a detected agent, in landscape")
	}

	if agents.Y0 <= sessions.Y1 {
		t.Errorf("agents panel is not stacked below sessions: sessions=%v agents=%v", sessions, agents)
	}

	if agents.X0 != sessions.X0 || agents.X1 != sessions.X1 {
		t.Errorf("agents panel does not share sessions' width: sessions=%v agents=%v", sessions, agents)
	}

	output, ok := dimensions[outputViewName]
	if !ok {
		t.Fatal("output view missing")
	}
	if output.X0 <= sessions.X1 {
		t.Errorf("output does not start after the sessions/agents column: sessions=%v output=%v", sessions, output)
	}
}

// Portrait mode is already contested for space, so the dashboard stays
// hidden there even with a detected agent.
func TestRootBoxHidesAgentsPanelInPortraitEvenWithDetectedAgent(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "a")
	sess.SetAgentState(agent.StateBlocked)

	dimensions := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 80, 50)

	if _, ok := dimensions[agentsViewName]; ok {
		t.Error("agents view is arranged in portrait mode despite a detected agent")
	}
}

// toggleZoom is the only way in and out: it flips the flag and moves focus
// to whichever view still exists.
func TestToggleZoomFlipsStateAndFocus(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.toggleZoom(g, nil); err != nil {
		t.Fatalf("toggleZoom: %v", err)
	}
	if !gui.zoomed {
		t.Fatal("toggleZoom did not zoom in")
	}
	if current := g.CurrentView(); current == nil || current.Name() != outputViewName {
		t.Errorf("current view after zooming in = %v, want %q", current, outputViewName)
	}

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout after zooming in: %v", err)
	}
	sessions, err := g.View(sessionsViewName)
	if err != nil {
		t.Fatalf("View(sessions): %v", err)
	}
	if sessions.Visible {
		t.Error("sessions view is still Visible while zoomed")
	}

	if err := gui.toggleZoom(g, nil); err != nil {
		t.Fatalf("toggleZoom (out): %v", err)
	}
	if gui.zoomed {
		t.Fatal("toggleZoom did not zoom back out")
	}
	if current := g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view after zooming out = %v, want %q", current, sessionsViewName)
	}

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout after zooming out: %v", err)
	}
	if !sessions.Visible {
		t.Error("sessions view was not made Visible again after zooming out")
	}
}
