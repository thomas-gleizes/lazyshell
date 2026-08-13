package gui

import (
	"fmt"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/i18n"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// detectedAgentSessions is every session with a detected AI agent, in
// creation order — gui.sessions.List()'s order, not displaySessions()'s
// group-rearranged one: the dashboard is a global view across every session,
// not a mirror of the sessions panel's current filter/grouping.
//
// Nil-safe on gui.sessions: rootBox calls this from ConditionalChildren to
// decide whether the panel has anything to show, and several tests build a
// Gui with New(nil, cfg) to exercise layout logic alone.
func (gui *Gui) detectedAgentSessions() []*session.Session {
	if gui.sessions == nil {
		return nil
	}

	return agentDashboardSessions(gui.sessions.List())
}

// agentDashboardSessions is detectedAgentSessions' pure filter, kept separate
// so it can be tested without a Manager.
func agentDashboardSessions(sessions []*session.Session) []*session.Session {
	out := make([]*session.Session, 0, len(sessions))

	for _, sess := range sessions {
		if sess.AgentState() != agent.StateNone {
			out = append(out, sess)
		}
	}

	return out
}

// agentStateLabel maps an agent.State to its translated label. "" for
// StateNone, which never reaches this panel in the first place.
func agentStateLabel(tr *i18n.Catalog, state agent.State) string {
	switch state {
	case agent.StateIdle:
		return tr.T("agents_panel.state_idle")
	case agent.StateWorking:
		return tr.T("agents_panel.state_working")
	case agent.StateBlocked:
		return tr.T("agents_panel.state_blocked")
	case agent.StateDone:
		return tr.T("agents_panel.state_done")
	default:
		return ""
	}
}

// agentRowText renders one session's dashboard line: the same colored/pulsed
// dot as the sessions panel's gutter (markers.agentMarker, colorizeMarker),
// the name, which agent CLI it is (sess.AgentName — "claude"/"codex"/
// "opencode"/... —, blank when not yet known), the state label, and the turn
// duration when TurnDuration reports one — only while the state is actually
// StateWorking, per its own contract.
func agentRowText(sess *session.Session, markers markerSet, tr *i18n.Catalog) string {
	glyph, sgrCode := markers.agentMarker(sess.AgentState())

	line := fmt.Sprintf("%s %-12s %-9s %s", colorizeMarker(glyph, sgrCode), sess.Name(), sess.AgentName(), agentStateLabel(tr, sess.AgentState()))

	if d, ok := sess.TurnDuration(); ok {
		line += "  ⏱ " + formatTurnDuration(d)
	}

	return line + "\n"
}

// agentsPanelContent renders the whole dashboard: one line per detected agent
// session, "" when there are none — a pure function of its inputs, the same
// separation sessionsPanelContent keeps from its gocui-writing caller.
func agentsPanelContent(sessions []*session.Session, markers markerSet, tr *i18n.Catalog) string {
	if len(sessions) == 0 {
		return ""
	}

	var b strings.Builder
	for _, sess := range sessions {
		b.WriteString(agentRowText(sess, markers, tr))
	}

	return b.String()
}

// renderAgentsPanel redraws the agents dashboard. Same shape as
// renderSessionsPanel but with no selection to track: the panel is never
// focusable, so there is no cursor to keep on screen.
func (gui *Gui) renderAgentsPanel() error {
	if gui.g == nil {
		return nil
	}

	content := agentsPanelContent(gui.detectedAgentSessions(), gui.markerSet(), gui.tr)

	if !gui.agentsPanelChanged(content) {
		return nil
	}

	gui.g.Update(func(g *gocui.Gui) error {
		view, err := g.View(agentsViewName)
		if err != nil {
			// Not created yet — the panel is hidden (no agent detected, or
			// portrait mode). Forget what we just recorded, so the next tick
			// draws instead of comparing against something never displayed.
			gui.invalidateAgentsPanel()

			return nil
		}

		applyAgentsPanelUpdate(view, content)

		return nil
	})

	return nil
}

// applyAgentsPanelUpdate writes content to the agents view. Split out from
// renderAgentsPanel's g.Update closure so it can be exercised directly in a
// test, the same reason applySessionsPanelUpdate is.
func applyAgentsPanelUpdate(view *gocui.View, content string) {
	view.Clear()
	fmt.Fprint(view, content)
}

// agentsPanelChanged reports whether the panel differs from what was last
// pushed, and records the new state as pushed — same diffing discipline as
// sessionsPanelChanged, guarded by the same mu since both run from goEvery's
// background goroutine.
func (gui *Gui) agentsPanelChanged(content string) bool {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.agentsDrawn && content == gui.lastAgentsContent {
		return false
	}

	gui.lastAgentsContent, gui.agentsDrawn = content, true

	return true
}

// invalidateAgentsPanel forgets what was last pushed, so the next render
// draws unconditionally.
func (gui *Gui) invalidateAgentsPanel() {
	gui.mu.Lock()
	gui.agentsDrawn = false
	gui.mu.Unlock()
}
