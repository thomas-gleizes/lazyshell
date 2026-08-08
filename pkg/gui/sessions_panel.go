package gui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/i18n"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// Default gutter markers, in the four columns every session line starts
// with. They are the only way to learn something about a session that is not
// the one on screen: what it is running, whether it asked for attention while
// hidden, whether it produced output while hidden, and whether it is marked
// for broadcast. All four are overridden by pkg/config's Markers; an empty
// override turns that marker off.
const (
	// bellMarker flags a session that emitted a BEL since it was last looked
	// at — a finished build, a shell asking a question.
	bellMarker = "!"
	// altScreenMarker flags a session with a full-screen application in
	// control (vim, htop, less).
	altScreenMarker = "#"
	// activityMarker flags a session, other than the one currently selected,
	// that produced output since it was last looked at.
	activityMarker = "●"
	// broadcastMarker flags a session marked to receive broadcast keystrokes
	// (pkg/gui/broadcast.go) — set on 2 or more sessions at once, it is what
	// arms the actual diffusion.
	broadcastMarker = "+"
	// agentIdleMarker/agentWorkingMarker/agentBlockedMarker/agentDoneMarker
	// flag a detected AI agent session's state (pkg/agent) — distinct from
	// activityMarker, which only means "produced output": these mean "is it
	// waiting on you". Rendered in color (see agentStateColor) on top of the
	// glyph, so the four states stay visually distinct even at a glance.
	agentIdleMarker    = "·"
	agentWorkingMarker = "…"
	agentBlockedMarker = "‼"
	agentDoneMarker    = "✓"
)

// gutterColumns is the gutter's width, in characters: one per marker kind.
// sessionsPanelContent pads to this manually rather than through a Printf
// width spec, because the agent marker's color escape codes are part of the
// string's byte length but must not count towards its visible width.
const gutterColumns = 5

// markerSet is the resolved set of gutter characters, so sessionsPanelContent
// stays a pure function of its inputs rather than reaching into a Gui.
type markerSet struct {
	bell      string
	altScreen string
	activity  string
	broadcast string

	agentIdle    string
	agentWorking string
	agentBlocked string
	agentDone    string
}

// markerSet resolves the configured markers against the built-in defaults. A
// marker the user explicitly set to "" stays empty — that is how you turn one
// off — which is why this cannot be a plain "empty means default" merge on the
// already-merged config: pkg/config's Default() has already filled all eight, so
// anything empty at this point was asked for.
func (gui *Gui) markerSet() markerSet {
	set := markerSet{
		bell:         gui.markers.Bell,
		altScreen:    gui.markers.AltScreen,
		activity:     gui.markers.Activity,
		broadcast:    gui.markers.Broadcast,
		agentIdle:    gui.markers.AgentIdle,
		agentWorking: gui.markers.AgentWorking,
		agentBlocked: gui.markers.AgentBlocked,
		agentDone:    gui.markers.AgentDone,
	}

	// The zero-value Gui case only: a bare struct literal (tests) never went
	// through config.Default, so every field is empty at once and means
	// "nothing was configured" rather than "all were turned off".
	if gui.markers == (config.Markers{}) {
		set.bell, set.altScreen, set.activity, set.broadcast = bellMarker, altScreenMarker, activityMarker, broadcastMarker
		set.agentIdle, set.agentWorking, set.agentBlocked, set.agentDone =
			agentIdleMarker, agentWorkingMarker, agentBlockedMarker, agentDoneMarker
	}

	return set
}

// agentMarker returns the configured glyph for state and the raw ANSI SGR
// foreground code to wrap it in — "" for agent.StateNone, which draws
// nothing, and for a state whose marker was explicitly turned off. Fixed,
// non-theme colors on purpose: they mirror herdr's idle/working/blocked/done
// convention (green/yellow/red/blue) rather than adding a themeable surface
// this phase's design report never asked for.
func (set markerSet) agentMarker(state agent.State) (glyph, sgrCode string) {
	switch state {
	case agent.StateIdle:
		glyph, sgrCode = set.agentIdle, "32"
	case agent.StateWorking:
		glyph, sgrCode = set.agentWorking, "33"
	case agent.StateBlocked:
		glyph, sgrCode = set.agentBlocked, "31"
	case agent.StateDone:
		glyph, sgrCode = set.agentDone, "34"
	default:
		return "", ""
	}

	if glyph == "" {
		return "", ""
	}

	return glyph, sgrCode
}

// colorizeMarker wraps ch in a raw ANSI SGR foreground code, reset
// immediately after — gocui has rendered in OutputTrue (truecolor) mode
// since phase 10, so an 8-color SGR sequence embedded directly in view
// content renders as intended. Returns ch unchanged if code is empty (marker
// turned off) or ch is empty (nothing to color).
func colorizeMarker(ch, sgrCode string) string {
	if ch == "" || sgrCode == "" {
		return ch
	}

	return "\x1b[" + sgrCode + "m" + ch + "\x1b[0m"
}

// sessionsPanelContent renders one line per session: a four-column gutter of
// markers, then name, status (or exit result), PID, and either the terminal
// title the shell set (usually the running command, which is what you
// actually want to read in a session list) or the working directory when it
// set none.
//
// Exactly one line per session is a hard constraint, not a style choice:
// renderSessionsPanel's view.SetCursor(0, selected) and the view's Highlight
// both address sessions by line number.
//
// broadcastMarks is which session IDs are currently marked for broadcast —
// nil (no marks at all) is a valid, common value, not a special case.
//
// A pure function, kept separate from the gocui-writing side so it can be
// tested directly, the same way keys.Translate and spike.edit are.
func sessionsPanelContent(sessions []*session.Session, markers markerSet, selectedID string, broadcastMarks map[string]bool, statsLines map[string]string, tr *i18n.Catalog) string {
	if len(sessions) == 0 {
		return tr.T("sessions.empty")
	}

	var b strings.Builder
	for _, sess := range sessions {
		pid := 0
		if sess.Cmd.Process != nil {
			pid = sess.Cmd.Process.Pid
		}

		detail := sess.Screen().Title()
		if detail == "" {
			detail = sess.Cwd
		}

		if d, ok := sess.TurnDuration(); ok {
			turn := "⏱ " + formatTurnDuration(d)
			if line := statsLines[sess.ID]; line != "" {
				turn += " · " + line
			}

			detail = turn + "  " + detail
		}

		gutter := sessionMarkers(sess, markers, sess.ID == selectedID, broadcastMarks[sess.ID])
		status := statusColumn(sess)

		// gutter is already padded to gutterColumns visible characters by
		// sessionMarkers (its raw byte length can run longer than that once
		// it carries a colorized marker) — %s here, not a width spec.
		fmt.Fprintf(&b, "%s%-12s %-8s %6d  %s\n", gutter, sess.Name(), status, pid, detail)
	}

	return b.String()
}

// formatTurnDuration renders a turn's elapsed time to whole seconds — a
// gutter/detail column is not the place for sub-second precision, and
// Duration's own String() would otherwise print e.g. "1m32.114837s".
func formatTurnDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}

// sessionMarkers builds a session's gutter, padded to gutterColumns visible
// characters. The activity marker never applies to the currently selected
// session: it would otherwise stay lit permanently on the one the user is
// looking at, since every byte of its output re-arms the underlying latch.
//
// The agent marker is appended last and, unlike the other four, may be
// wrapped in color — which is why padding happens here, on the plain-text
// length, rather than through a Printf width spec in the caller: an SGR
// sequence's bytes must not count towards the column it visually occupies.
func sessionMarkers(sess *session.Session, markers markerSet, isSelected, isBroadcastMarked bool) string {
	gutter := ""

	if sess.Screen().BellPending() {
		gutter += markers.bell
	}

	if sess.Screen().IsAltScreen() {
		gutter += markers.altScreen
	}

	if !isSelected && sess.Screen().ActivityPending() {
		gutter += markers.activity
	}

	if isBroadcastMarked {
		gutter += markers.broadcast
	}

	visibleLen := len([]rune(gutter))

	if glyph, sgrCode := markers.agentMarker(sess.AgentState()); glyph != "" {
		gutter += colorizeMarker(glyph, sgrCode)
		visibleLen++
	}

	if pad := gutterColumns - visibleLen; pad > 0 {
		gutter += strings.Repeat(" ", pad)
	}

	return gutter
}

// statusColumn renders a session's status field: its result once it has
// exited (✓, or ✗ with the exit code), the running/exited word otherwise.
func statusColumn(sess *session.Session) string {
	if sess.Status() != session.StatusExited {
		return sess.Status().String()
	}

	if sess.ExitCode() == 0 {
		return "✓"
	}

	return fmt.Sprintf("✗ %d", sess.ExitCode())
}

// selectedSession returns the session at the current selection, clamped to
// the current list bounds (the list can shrink under us — a session can exit
// or be killed while it is selected, or a filter can hide it), or nil if
// there is none. Addresses gui.filteredSessions(), not the manager's raw
// list, so this always agrees with what is actually on screen.
func (gui *Gui) selectedSession() *session.Session {
	sessions := gui.filteredSessions()
	if len(sessions) == 0 {
		return nil
	}

	index := gui.getSelectedIndex()
	if index >= len(sessions) {
		index = len(sessions) - 1
	}

	if index < 0 {
		index = 0
	}

	gui.setSelectedIndex(index)

	return sessions[index]
}

// renderSessionsPanel redraws the session list and keeps the cursor on the
// selected line. Called on a ticker (statuses change asynchronously in the
// background) and after every action that changes the list or the selection.
//
// Scheduled through g.Update rather than written directly: View.Write/Clear
// are safe from any goroutine (an internal writeMutex), but View.SetCursor
// is a plain field write with no such protection, and goEvery drives this
// from its own background goroutine — mutating it outside g.Update would
// race against gocui's own render pass reading it during MainLoop.
//
// An unchanged panel is not pushed at all: a g.Update redraws the whole
// screen, and this runs on a 30 ms ticker whether or not any session status,
// title or marker has moved.
func (gui *Gui) renderSessionsPanel() error {
	if gui.g == nil {
		return nil
	}

	selectedID := ""
	if sess := gui.selectedSession(); sess != nil {
		selectedID = sess.ID
		// The selected session is being watched live, so any activity it
		// produces while it has focus is by definition already seen — clear
		// it on every tick rather than only on selection change, or the
		// marker would light up the instant the user looks away from a
		// session that kept producing output the whole time they were on it.
		sess.Screen().ClearActivity()
	}

	content := sessionsPanelContent(gui.filteredSessions(), gui.markerSet(), selectedID, gui.broadcastMarks, gui.statsLinesForRender(), gui.tr)
	selected := gui.getSelectedIndex()

	if !gui.sessionsPanelChanged(content, selected) {
		return nil
	}

	gui.g.Update(func(g *gocui.Gui) error {
		view, err := g.View(sessionsViewName)
		if err != nil {
			// Not created yet — the first layout pass has not run. Forget what
			// we just recorded, so the next tick draws instead of comparing
			// against something that was never displayed.
			gui.invalidateSessionsPanel()

			return nil
		}

		applySessionsPanelUpdate(view, content, selected)

		return nil
	})

	return nil
}

// applySessionsPanelUpdate writes content to the sessions view and scrolls it
// so the selected line stays visible. Split out from renderSessionsPanel's
// g.Update closure so it can be exercised directly in a test — that closure
// only runs once gocui's MainLoop drains it, which headless tests never do.
//
// FocusPoint, not SetCursor: SetCursor takes a position relative to the
// current origin and never moves that origin, so once the list grows past the
// panel's height the origin stays pinned at 0 forever — everything past the
// first InnerHeight lines is simply never drawn, selected or not. FocusPoint
// takes the absolute line instead and scrolls the origin just enough to keep
// it on screen (centering only when it was outside the visible window), the
// same list-follows-selection behaviour j/k already implies.
func applySessionsPanelUpdate(view *gocui.View, content string, selected int) {
	view.Clear()
	fmt.Fprint(view, content)
	view.FocusPoint(0, selected, true)
}

// sessionsPanelChanged reports whether the panel differs from what was last
// pushed, and records the new state as pushed. Guarded by gui.mu because
// renderSessionsPanel is called both from goEvery's background goroutine and
// from keybinding handlers on gocui's.
func (gui *Gui) sessionsPanelChanged(content string, selected int) bool {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.sessionsDrawn && content == gui.lastSessionsContent && selected == gui.lastSessionsSelected {
		return false
	}

	gui.lastSessionsContent, gui.lastSessionsSelected, gui.sessionsDrawn = content, selected, true

	return true
}

// invalidateSessionsPanel forgets what was last pushed, so the next render
// draws unconditionally.
func (gui *Gui) invalidateSessionsPanel() {
	gui.mu.Lock()
	gui.sessionsDrawn = false
	gui.mu.Unlock()
}

// selectionMoved moves the selection by delta (j/k, arrows), clamped to the
// list bounds, and shows the newly selected session's output.
func (gui *Gui) selectionMoved(delta int) func(*gocui.Gui, *gocui.View) error {
	return func(*gocui.Gui, *gocui.View) error {
		sessions := gui.filteredSessions()
		if len(sessions) == 0 {
			return nil
		}

		index := gui.getSelectedIndex() + delta
		if index < 0 {
			index = 0
		}
		if index >= len(sessions) {
			index = len(sessions) - 1
		}

		return gui.applySelection(index)
	}
}

// jumpToNextBlockedSession moves the selection to the next session whose
// agent.State is StateBlocked, cycling from just after the current
// selection and wrapping once — the ergonomics phase 11c's whole
// notification/detection stack exists for: at 6 agent sessions open, this
// is how you actually get to the one that is waiting on you, instead of
// paging through the list by hand.
//
// Silently does nothing if none are blocked — same convention as
// selectIndex's out-of-range no-op: a false "nothing to see here" beep for
// what is, most of the time, the normal state would be worse than silence.
func (gui *Gui) jumpToNextBlockedSession(*gocui.Gui, *gocui.View) error {
	sessions := gui.filteredSessions()
	if len(sessions) == 0 {
		return nil
	}

	start := gui.getSelectedIndex()

	for i := 1; i <= len(sessions); i++ {
		index := (start + i) % len(sessions)

		if sessions[index].AgentState() == agent.StateBlocked {
			return gui.applySelection(index)
		}
	}

	return nil
}

// selectIndex jumps straight to the i-th session (0-based) — the "1"-"9"
// direct-jump keys. Silently does nothing if the list is shorter than i, the
// same way selectionMoved silently clamps rather than erroring: pressing "7"
// with 5 sessions open is not a mistake worth interrupting the user over.
func (gui *Gui) selectIndex(i int) func(*gocui.Gui, *gocui.View) error {
	return func(*gocui.Gui, *gocui.View) error {
		if i >= len(gui.filteredSessions()) {
			return nil
		}

		return gui.applySelection(i)
	}
}

// applySelection is the common tail of every absolute or relative move: set
// the index, show the newly selected session's output, redraw the list.
func (gui *Gui) applySelection(index int) error {
	gui.setSelectedIndex(index)

	gui.onSelectionChanged()

	return gui.renderSessionsPanel()
}

// onSelectionChanged starts rendering the newly selected session's output,
// replacing whichever session was being rendered before. Scroll always
// resets to live: an old scroll position from a different session would be
// meaningless here.
func (gui *Gui) onSelectionChanged() {
	gui.setScrollOffset(0)
	gui.clearSearch()

	if sess := gui.selectedSession(); sess != nil {
		gui.debug.Event("selection → %s (%s)", sess.Name(), sess.ID)

		// Looking at a session is what acknowledges its bell and its activity:
		// both markers exist to say "this one moved while you were elsewhere".
		sess.Screen().ClearBell()
		sess.Screen().ClearActivity()
		gui.showOutput(sess)
	}

	gui.updateWindowTitle()
}

// newSession creates a session running the user's shell, in lazyshell's own
// working directory, and selects it. A creation failure is shown in the
// status bar rather than propagated.
func (gui *Gui) newSession(*gocui.Gui, *gocui.View) error {
	gui.sessionCounter++
	name := fmt.Sprintf("session-%d", gui.sessionCounter)

	if _, err := gui.sessions.New(name, gui.defaultShell()); err != nil {
		return gui.reportSessionError(err)
	}

	gui.debug.Event("session %s created (%s)", name, gui.defaultShell())

	gui.lastError = ""
	gui.lastInfo = ""

	return gui.selectNewlyCreatedSession()
}

// killSession asks for confirmation before killing the selected session.
func (gui *Gui) killSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showConfirm(gui.tr.T("sessions.kill_confirm", sess.Name()), func() error {
		gui.debug.Event("session %s (%s) killed", sess.Name(), sess.ID)

		return gui.sessions.Kill(sess.ID)
	})
}

// deleteSession asks for confirmation before permanently removing the
// selected session: unlike killSession, it also drops the session from the
// panel — it stops showing up at all, running or exited, and cannot be
// restarted afterwards.
func (gui *Gui) deleteSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showConfirm(gui.tr.T("sessions.delete_confirm", sess.Name()), func() error {
		// The resources tab's series are keyed by session id; a deleted
		// session's would otherwise stay in memory for the life of the
		// process, and a later session reusing the id would inherit them.
		gui.forgetPerfHistory(sess.ID)

		return gui.sessions.Remove(sess.ID)
	})
}

// restartSession re-creates the selected session if it has exited — same
// name, shell, cwd, env and initial command as when it was created. Refuses
// (rather than silently no-op) on a session that is still running, so the
// user learns why nothing happened instead of wondering.
func (gui *Gui) restartSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	if sess.Status() != session.StatusExited {
		return gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("sessions.restart_running", sess.Name())))
	}

	if _, err := gui.sessions.Restart(sess.ID); err != nil {
		return gui.reportSessionError(err)
	}

	gui.lastError = ""
	gui.lastInfo = ""
	gui.onSelectionChanged()

	// A restart is a fresh shell like any other creation, and it is what the
	// user reaches for right after watchSelectedExit dropped them back on the
	// exited session — so it lands them back inside it.
	if err := gui.focusSelectedShell(); err != nil {
		return err
	}

	return gui.renderSessionsPanel()
}

// renameSession prompts for a new name, pre-filled with the current one — the
// "renommage de session" ergonomics feature. Purely cosmetic: SetName does
// not touch the running shell.
func (gui *Gui) renameSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showPrompt(gui.tr.T("prompt.rename"), sess.Name(), func(newName string) error {
		if newName == "" {
			return nil
		}

		sess.SetName(newName)

		return nil
	})
}

// duplicateSession immediately starts a new session with the same shell and
// working directory as the selected one — no prompt, unlike newSessionInDir,
// since there is nothing to ask the user.
func (gui *Gui) duplicateSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	if _, err := gui.sessions.NewInDir(sess.Name()+"-copie", gui.defaultShell(), sess.Cwd); err != nil {
		return gui.reportSessionError(err)
	}

	gui.lastError = ""
	gui.lastInfo = ""

	return gui.selectNewlyCreatedSession()
}

// newSessionInDir prompts for a working directory, pre-filled with
// lazyshell's own cwd, and starts a session there — the "session dans un cwd
// choisi" ergonomics feature.
func (gui *Gui) newSessionInDir(*gocui.Gui, *gocui.View) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}

	return gui.showPrompt(gui.tr.T("prompt.new_in_dir"), cwd, func(dir string) error {
		if dir == "" {
			return nil
		}

		gui.sessionCounter++
		name := fmt.Sprintf("session-%d", gui.sessionCounter)

		if _, err := gui.sessions.NewInDir(name, gui.defaultShell(), dir); err != nil {
			return err
		}

		return gui.selectNewlyCreatedSession()
	})
}

// selectNewlyCreatedSession moves the selection to the last session in the
// manager's list (creation always appends) and starts rendering its output —
// the common tail of newSession, duplicateSession and newSessionInDir.
//
// Clears any active filter first: a filter that does not happen to match the
// name of a session just created would otherwise hide the very thing the
// user just asked for, with setSelectedIndex pointing at an index in a
// filtered list the new session was never added to.
func (gui *Gui) selectNewlyCreatedSession() error {
	gui.filterPattern = ""
	gui.setSelectedIndex(len(gui.sessions.List()) - 1)
	gui.onSelectionChanged()

	if err := gui.focusSelectedShell(); err != nil {
		return err
	}

	return gui.renderSessionsPanel()
}

// focusSelectedShell hands the keyboard to the selected session's shell:
// focus to the output panel, pass-through armed. The counterpart of
// watchSelectedExit (pkg/gui/exit_watch.go), which does the reverse when a
// shell ends — starting a session is only ever followed by typing into it, so
// the panel-then-Tab-then-i sequence is three keystrokes of pure ceremony.
//
// Called from the creation paths only (newSession, duplicateSession,
// newSessionInDir, restartSession): moving the selection with j/k stays a
// navigation gesture and must not arm pass-through under the user.
//
// enterPassThrough calls onSelectionChanged again, after the callers here
// already did — harmless (it resets the scroll to live and restarts the render
// task, both idempotent) and preferable to a second way into pass-through
// that could drift from this one.
func (gui *Gui) focusSelectedShell() error {
	if gui.g == nil || gui.selectedSession() == nil {
		return nil
	}

	if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
		return err
	}

	gui.enterPassThrough()

	return nil
}

// reportSessionError shows err in the status bar in place of the keybinding
// hint, the same way every session-creation failure is surfaced. Clears any
// leftover success message — an old "exported to ..." must not keep
// competing with a new error for the same line.
func (gui *Gui) reportSessionError(err error) error {
	gui.lastError = err.Error()
	gui.lastInfo = ""

	if view, verr := gui.g.View(statusViewName); verr == nil {
		gui.renderStatus(view)
	}

	return nil
}

// reportSessionInfo is reportSessionError's positive counterpart: a
// transient success message (currently only the export path) shown in the
// status bar in place of the keybinding hint. Kept as a separate field and
// function rather than folding "success" into lastError so a reader is
// never left wondering which kind the string currently holds.
func (gui *Gui) reportSessionInfo(msg string) error {
	gui.lastInfo = msg
	gui.lastError = ""

	if view, verr := gui.g.View(statusViewName); verr == nil {
		gui.renderStatus(view)
	}

	return nil
}

// defaultShell resolves the shell to start new sessions with: pkg/config's
// Shell if set, else $SHELL, else /bin/bash — mirrors cmd/spike-pty's shell()
// helper, duplicated here because that command is package main and cannot be
// imported.
func (gui *Gui) defaultShell() string {
	if gui.configuredShell != "" {
		return gui.configuredShell
	}

	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	return "/bin/bash"
}
