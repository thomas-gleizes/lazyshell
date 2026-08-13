package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// testAgentMarkers is the built-in agent gutter glyphs, no colors configured
// — agentMarker's own fallback still produces an SGR code once the glyph
// itself is non-empty, so colorizeMarker's output is still testable.
var testAgentMarkers = markerSet{
	agentIdle:    agentIdleMarker,
	agentWorking: agentWorkingMarker,
	agentBlocked: agentBlockedMarker,
	agentDone:    agentDoneMarker,
}

func newAgentTestManager(t *testing.T) *session.Manager {
	t.Helper()

	m := session.NewManager()
	m.KillTimeout = 1 * time.Second
	t.Cleanup(m.Shutdown)

	return m
}

func TestAgentDashboardSessionsFiltersToDetectedAgentsOnly(t *testing.T) {
	m := newAgentTestManager(t)

	if _, err := m.New("a", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := m.New("b", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, err := m.New("c", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	b.SetAgentState(agent.StateWorking)
	c.SetAgentState(agent.StateBlocked)

	got := agentDashboardSessions(m.List())

	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2: %v", len(got), got)
	}

	// Creation order, the same as m.List() — not displaySessions()'s
	// group-rearranged one.
	if got[0].ID != b.ID || got[1].ID != c.ID {
		t.Errorf("got %s then %s, want creation order b then c", got[0].Name(), got[1].Name())
	}
}

func TestAgentDashboardSessionsEmptyWhenNoneDetected(t *testing.T) {
	m := newAgentTestManager(t)

	if _, err := m.New("a", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := agentDashboardSessions(m.List()); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// rootBox calls detectedAgentSessions from ConditionalChildren, and several
// config-wiring tests build a Gui with New(nil, cfg) to exercise layout logic
// alone — this must not panic.
func TestDetectedAgentSessionsNilManagerIsSafe(t *testing.T) {
	var gui Gui

	if got := gui.detectedAgentSessions(); got != nil {
		t.Errorf("got %v, want nil for a Gui with no session manager", got)
	}
}

func TestAgentsPanelContentEmptyWhenNoAgents(t *testing.T) {
	if got := agentsPanelContent(nil, testAgentMarkers, nil); got != "" {
		t.Errorf("content = %q, want empty", got)
	}
}

// A nil *i18n.Catalog resolves as French (Catalog.T's own contract), which is
// what this asserts on rather than hardcoding an i18n.New call.
func TestAgentsPanelContentShowsNameStateColorAndDuration(t *testing.T) {
	m := newAgentTestManager(t)

	working, err := m.New("working", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	working.SetAgentState(agent.StateWorking)

	done, err := m.New("done", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done.SetAgentState(agent.StateDone)

	content := agentsPanelContent([]*session.Session{working, done}, testAgentMarkers, nil)

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per session:\n%q", len(lines), lines)
	}

	if !strings.Contains(lines[0], working.Name()) {
		t.Errorf("working line = %q, want the session name", lines[0])
	}

	if !strings.Contains(lines[0], "\x1b[") {
		t.Errorf("working line = %q, want a colored marker", lines[0])
	}

	if !strings.Contains(lines[0], "en cours") {
		t.Errorf("working line = %q, want the working state label", lines[0])
	}

	if !strings.Contains(lines[0], "⏱") {
		t.Errorf("working line = %q, want a turn duration", lines[0])
	}

	if !strings.Contains(lines[1], done.Name()) {
		t.Errorf("done line = %q, want the session name", lines[1])
	}

	if !strings.Contains(lines[1], "terminé") {
		t.Errorf("done line = %q, want the done state label", lines[1])
	}

	// TurnDuration only reports ok while the state is StateWorking — done has
	// already left it.
	if strings.Contains(lines[1], "⏱") {
		t.Errorf("done line = %q, want no turn duration outside StateWorking", lines[1])
	}
}

// renderAgentsPanel runs on a ticker that starts before the first layout
// pass, so it must tolerate a missing view.
func TestRenderAgentsPanelWithoutView(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if err := gui.renderAgentsPanel(); err != nil {
		t.Fatalf("renderAgentsPanel: %v", err)
	}
}

// Exercises the real path: a detected agent makes rootBox include the
// agents view, layout creates it, and applyAgentsPanelUpdate (the piece
// renderAgentsPanel's g.Update closure delegates to, untestable directly
// since nothing here pumps gocui's MainLoop) writes the dashboard into it.
func TestApplyAgentsPanelUpdateWritesDetectedAgentsToTheView(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess := newTestSession(t, gui, "agent-session")
	sess.SetAgentState(agent.StateBlocked)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := g.View(agentsViewName)
	if err != nil {
		t.Fatalf("View(agents): %v", err)
	}

	content := agentsPanelContent(gui.detectedAgentSessions(), gui.markerSet(), gui.tr)
	applyAgentsPanelUpdate(view, content)

	if got := view.Buffer(); !strings.Contains(got, sess.Name()) {
		t.Errorf("agents view content = %q, want it to mention %q", got, sess.Name())
	}
}
