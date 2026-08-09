package gui

import (
	"fmt"
	"os"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/keys"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// defaultPrefixKey is the escape key that leaves pass-through mode. Every
// other key reaches the shell while pass-through is active, so this is the
// only way back on the keyboard — overridable through LAZYSHELL_PREFIX,
// because a terminal multiplexer running above lazyshell may well eat it
// before lazyshell ever sees it.
//
// Ctrl-O, not the tmux-style Ctrl-B phase 1 shipped (cmd/spike-pty, ADR 0001
// decision 3): Ctrl-B turned out to be a key the sessions themselves want.
// Claude Code binds it to "run this bash command in the background", so a user
// pressing it inside an agent session was pressing a key that means something
// on both sides at once. See docs/adr/0004-sortie-du-pass-through.md.
const defaultPrefixKey = gocui.KeyCtrlO

// escExitWindow is how long a first Escape stays eligible to be completed by a
// second one into a pass-through exit. It exists because the pair has to be a
// deliberate double press: without a deadline, an Escape typed into vim now and
// another one five minutes later would add up to an exit, which is the
// never-expiring armed state ADR 0004 removed.
//
// The value is a compromise, and the losing side is stated plainly: vim users
// who double-tap Escape out of habit will leave pass-through. That is the price
// of a second exit that needs no key to be learnt, and Ctrl-O (which no
// application below claims) stays the exit for anyone who finds it wrong.
const escExitWindow = 400 * time.Millisecond

// prefixFrom resolves the pass-through escape key: $LAZYSHELL_PREFIX wins
// if set (a terminal multiplexer running above lazyshell may well eat the key
// before lazyshell ever sees it, so the env var has to be able to override a
// config file too), then cfgValue (pkg/config's PrefixKey), then the
// built-in default. Both are in gocui.Parse syntax: "Ctrl+A", "Ctrl+Space"...
// An unparseable value at either level falls through rather than leaving the
// user with no way out of pass-through mode.
func prefixFrom(cfgValue string) gocui.Key {
	if key, ok := parsePrefixKey(os.Getenv("LAZYSHELL_PREFIX")); ok {
		return key
	}

	if key, ok := parsePrefixKey(cfgValue); ok {
		return key
	}

	return defaultPrefixKey
}

func parsePrefixKey(name string) (gocui.Key, bool) {
	if name == "" {
		return 0, false
	}

	parsed, _, err := gocui.Parse(name)
	if err != nil {
		return 0, false
	}

	key, ok := parsed.(gocui.Key)

	return key, ok
}

// prefixName renders a prefix key for the status bar hint.
func prefixName(key gocui.Key) string {
	if key >= gocui.KeyCtrlA && key <= gocui.KeyCtrlZ {
		return fmt.Sprintf("Ctrl-%c", 'A'+(key-gocui.KeyCtrlA))
	}

	return fmt.Sprintf("touche %d", key)
}

// scrollHalfPage is the default divisor applied to the output view's row count
// for a Ctrl-U/Ctrl-D scroll — the "half" the keys are named after. Overridden
// by pkg/config's scroll.half_page_divisor.
const scrollHalfPage = 2

// pageStep is how many lines PgUp/PgDn move by, given the output view's current
// height: a full page unless scroll.page_lines asks for a fixed number.
func (gui *Gui) pageStep(rows int) int {
	if gui.scroll.PageLines > 0 {
		return gui.scroll.PageLines
	}

	return rows
}

// halfPageStep is how many lines Ctrl-U/Ctrl-D move by. The divisor is never
// allowed to reach zero here — Config.Validate rejects it, but a Gui built as a
// bare struct literal (tests) has not been through validation.
func (gui *Gui) halfPageStep(rows int) int {
	divisor := gui.scroll.HalfPageDivisor
	if divisor < 1 {
		divisor = scrollHalfPage
	}

	return rows / divisor
}

// editOutput is the sole keystroke handler for the output view (wired in
// initView as its permanent Editor). It cannot be split into ordinary
// SetKeybinding entries: gocui always lets a view-scoped keybinding win
// before ever consulting the Editor, so any keybinding registered on this
// view — even just for "i" — would fire during pass-through too and block
// that key from ever reaching the shell. Everything modal therefore lives
// here, mirroring cmd/spike-pty's edit() but against a real session.Manager
// selection instead of a single hardcoded pty.
func (gui *Gui) editOutput(view *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	// This guard is what makes g.Mouse = true safe, and it has to come first.
	// gocui routes a mouse event through the current view's Editor whenever no
	// mouse binding claimed it (gui.go's execKeybindings), and MouseLeft is the
	// very same value as KeyShiftArrowDown — so anything reaching
	// keys.Translate below would be encoded as "\x1b[1;2B" and typed into the
	// shell. Mouse events belong to mouse.go's bindings, never here.
	//
	// The cost is stated in docs/adr/0003-souris.md: while the mouse is on,
	// Shift-Up/Shift-Down can no longer be forwarded to a session. The test on
	// mouse.Enabled is what gives them back when it is off — gocui then emits
	// no mouse event at all, so a MouseLeft-valued key can only be a genuine
	// Shift-Down and must go through.
	if gui.mouse.Enabled && gocui.IsMouseKey(key) {
		// Logged separately and not through logKeyEvent: naming this value is
		// exactly what must not happen here (it is MouseLeft *and*
		// KeyShiftArrowDown), and the interesting fact is that it was dropped.
		gui.debug.Key("mouse event dropped by the Editor (key=%d mod=%d)", key, mod)

		return false
	}

	// Before Normalize, so the log shows what the terminal actually sent
	// alongside what lazyshell made of it — the two disagreeing is the usual
	// reason a combination does not do what it should.
	gui.logKeyEvent(key, ch, mod)

	key, ch, mod = keys.Normalize(key, ch, mod)

	if gui.passThroughActive {
		return gui.editDuringPassThrough(key, ch, mod)
	}

	return gui.editDuringScroll(view, key, ch)
}

// editDuringPassThrough forwards everything to the selected session except the
// escape key, which leaves pass-through on the spot.
//
// One press, one effect: phase 1's two-step automaton (prefix arms, the next
// key confirms) is deliberately gone. It made the exit unusable in practice —
// the first press changed nothing visible, so the natural reaction was to press
// again, which was precisely the sequence that stayed in pass-through and typed
// a literal control byte into the session instead. Any second key also had to
// be swallowed to end the sequence, so a user who pressed the prefix by mistake
// lost the next keystroke as well.
//
// The cost, stated plainly: the escape key can no longer be typed into a
// session at all, since there is no sequence left that means "send it
// literally". That is what makes it a config value (prefix_key,
// $LAZYSHELL_PREFIX) — a user who needs Ctrl-O in their shell moves the escape
// key elsewhere rather than losing the exit.
//
// Escape twice in a row is a second, discoverable exit (ADR 0005). It is not a
// prefix automaton coming back: the first Escape is forwarded to the session
// immediately like any other key, so Escape keeps working in vim and Claude
// Code, and nothing is armed that could swallow an unrelated keystroke.
func (gui *Gui) editDuringPassThrough(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	// ch == 0 matters: a control key arrives with no rune, while every
	// printable character arrives as key 0 with the rune set. Without this
	// test a prefix that happens to be key 0 (Ctrl-Space, which tcell encodes
	// as NUL) would match every letter typed and drop the user out of
	// pass-through on the first keystroke. Validate rejects a non-control
	// prefix_key, but $LAZYSHELL_PREFIX does not go through it.
	//
	// Checked before the Escape pair so that a user who set prefix_key to Esc
	// gets the single press they asked for.
	if key == gui.prefixKey && ch == 0 {
		gui.debug.Action("editor exit_pass_through (prefix %s)", prefixName(gui.prefixKey))
		gui.exitPassThrough()

		return true
	}

	if key == gocui.KeyEsc && ch == 0 {
		if gui.escPressedRecently() {
			// Only the second Escape is swallowed. The first already reached
			// the session, which is what makes the pair safe to type: leaving
			// pass-through this way costs the session one Escape, never zero
			// and never two.
			gui.debug.Action("editor exit_pass_through (Esc Esc)")
			gui.exitPassThrough()

			return true
		}

		gui.lastEscAt = time.Now()
		gui.dispatchKey(key, ch, mod)

		return true
	}

	// Any other key breaks the pair: Esc, j, Esc is a vim user moving around,
	// not someone asking to leave.
	gui.lastEscAt = time.Time{}

	gui.dispatchKey(key, ch, mod)

	return true
}

// escPressedRecently reports whether the Escape now being handled closes a pair
// with the previous one, i.e. whether the two are within escExitWindow. A zero
// lastEscAt (no Escape yet, or the pair was broken) never qualifies.
func (gui *Gui) escPressedRecently() bool {
	return !gui.lastEscAt.IsZero() && time.Since(gui.lastEscAt) < escExitWindow
}

// dispatchKey writes a translated keystroke to the selected session, or —
// once broadcastArmed — to every marked session at once, each translated
// separately: application cursor key mode (DECCKM) is per-session emulator
// state, so a session in that mode and one that isn't would need two
// different encodings of the very same arrow key.
func (gui *Gui) dispatchKey(key gocui.Key, ch rune, mod gocui.Modifier) {
	sess := gui.selectedSession()
	if sess == nil {
		return
	}

	if !gui.broadcastArmed(sess.ID) {
		gui.writeToSelected(gui.translate(key, ch, mod))

		return
	}

	for _, target := range gui.broadcastMarkedSessions() {
		gui.writeToSession(target, keys.TranslateWithMode(key, ch, mod, target.Screen().ApplicationCursorKeys()))
	}
}

// translate encodes a key event for the selected session, honouring the
// application cursor key mode (DECCKM) that session's emulator is currently
// in: vim and less arm it on entry, and some applications only accept the SS3
// arrow encoding it calls for.
func (gui *Gui) translate(key gocui.Key, ch rune, mod gocui.Modifier) []byte {
	appCursorKeys := false
	if sess := gui.selectedSession(); sess != nil {
		appCursorKeys = sess.Screen().ApplicationCursorKeys()
	}

	return keys.TranslateWithMode(key, ch, mod, appCursorKeys)
}

// editDuringScroll handles the output view outside pass-through: only a few
// keys are meaningful here, everything else falls through to gocui's normal
// keybinding dispatch (global Tab, global Ctrl-C...).
func (gui *Gui) editDuringScroll(view *gocui.View, key gocui.Key, ch rune) bool {
	// InnerHeight, not Size: Size counts the frame's two border rows, which
	// is not how many lines of the emulator are actually on screen — see
	// propagateResize's comment in layout.go for the same distinction on the
	// pty side. Using Size() here overshoots every PgUp/PgDn by two lines,
	// and lets copy-mode's auto-scroll window (moveCopyCursor) believe two
	// more lines are visible than really are.
	_, rows := view.InnerSize()

	// Only the output tab is a terminal. On perf/env the panel is a static
	// report about the session, so typing into it, selecting lines out of it
	// or searching a scrollback it is not showing are all meaningless — those
	// keys are simply not handled here, and fall through to gocui's ordinary
	// dispatch (Tab, Ctrl-C) like any other unclaimed key.
	if gui.outputTab != tabTerminal {
		return gui.editOnSecondaryTab(view, key, ch)
	}

	// Every branch below announces itself to the debug log. These actions are
	// invoked from inside the Editor and so never pass through
	// setKeybindings' wrapper — without a line per case, the log would show
	// the keystroke arriving and nothing happening. Spelled out rather than
	// derived, because "which case matched" is the exact question being asked.
	switch {
	// Copy-mode's own keys come first and win over everything below: while
	// selecting, j/k/arrows extend the selection instead of doing nothing,
	// and Esc cancels the selection rather than falling through to search's
	// Esc handler (searchActive() and copyModeActive are not expected to
	// overlap, but the order settles it either way).
	case gui.copyModeActive && key == gocui.KeyEsc && ch == 0:
		gui.debug.Action("editor cancel_copy_mode")
		gui.cancelCopyMode()

		return true
	case gui.copyModeActive && (ch == 'y' || ch == 'v'):
		gui.debug.Action("editor yank_copy_selection")
		gui.yankCopySelection()

		return true
	case gui.copyModeActive && (ch == 'j' || key == gocui.KeyArrowDown):
		gui.debug.Action("editor move_copy_cursor +1")
		gui.moveCopyCursor(1, rows)

		return true
	case gui.copyModeActive && (ch == 'k' || key == gocui.KeyArrowUp):
		gui.debug.Action("editor move_copy_cursor -1")
		gui.moveCopyCursor(-1, rows)

		return true

	// Below copy mode's keys on purpose: while selecting, the arrows belong
	// to the selection, and leaving the panel mid-selection is not what ← is
	// asking for.
	case key == gocui.KeyArrowLeft && !gui.copyModeActive:
		gui.debug.Action("editor focus_sessions")
		_ = gui.focusSessionsPanel()

		return true

	case (ch == 'i' || key == gocui.KeyEnter) && !gui.copyModeActive:
		gui.debug.Action("editor enter_pass_through")
		gui.enterPassThrough()

		return true

	case ch == 'v' && !gui.copyModeActive:
		gui.debug.Action("editor enter_copy_mode")
		gui.enterCopyMode()

		return true

	case key == gocui.KeyPgup && !gui.copyModeActive:
		gui.debug.Action("editor scroll page_up")
		gui.scrollBy(gui.pageStep(rows))

		return true
	case key == gocui.KeyCtrlU && !gui.copyModeActive:
		gui.debug.Action("editor scroll half_page_up")
		gui.scrollBy(gui.halfPageStep(rows))

		return true

	case key == gocui.KeyPgdn && !gui.copyModeActive:
		gui.debug.Action("editor scroll page_down")
		gui.scrollBy(-gui.pageStep(rows))

		return true
	case key == gocui.KeyCtrlD && !gui.copyModeActive:
		gui.debug.Action("editor scroll half_page_down")
		gui.scrollBy(-gui.halfPageStep(rows))

		return true

	case ch == '/' && !gui.copyModeActive:
		gui.debug.Action("editor show_search")
		_ = gui.showSearch(gui.g, view)

		return true

	case gui.searchActive() && ch == 'n':
		gui.debug.Action("editor next_match +1")
		gui.nextMatch(1)

		return true
	case gui.searchActive() && ch == 'N':
		gui.debug.Action("editor next_match -1")
		gui.nextMatch(-1)

		return true

	case gui.searchActive() && key == gocui.KeyEsc && ch == 0:
		gui.debug.Action("editor clear_search")
		gui.clearSearch()
		gui.restartOutput()
		gui.refreshSearchStatus()

		return true

	case ch == 'q':
		gui.debug.Action("editor quit")
		// A plain 'q' global keybinding can never fire as a fallback while
		// the current view is Editable — gocui excludes printable-character
		// global bindings from that path unconditionally. So quitting from
		// here has to be triggered by hand, the same way cmd/spike-pty's own
		// quit() does it.
		gui.g.Update(func(*gocui.Gui) error { return gocui.ErrQuit })

		return true

	case gui.matchesAction("help", key, ch):
		// Same reasoning as 'q' above: the global "help" binding cannot fire
		// as a fallback while this view is Editable, so it is matched and
		// triggered here by hand instead.
		gui.debug.Action("editor help")
		_ = gui.showHelp(gui.g, view)

		return true

	case gui.matchesAction("zoom", key, ch):
		// "zoom" is scoped to sessionsViewName, not global, but the same
		// problem applies: this view is Editable, so SetKeybinding is never
		// consulted for it regardless of scope. This is also the only way
		// back out of a zoomed output view — the sessions view that would
		// otherwise own this key does not exist while zoomed.
		gui.debug.Action("editor zoom")
		_ = gui.toggleZoom(gui.g, view)

		return true

	case gui.matchesAction("next_tab", key, ch):
		// Same reasoning again: these are scoped to sessionsViewName, and this
		// view being Editable means SetKeybinding is never consulted for it.
		gui.debug.Action("editor next_tab")
		gui.switchTab(1)

		return true
	case gui.matchesAction("prev_tab", key, ch):
		gui.debug.Action("editor prev_tab")
		gui.switchTab(-1)

		return true

	case gui.matchesAction("jump_prev_prompt", key, ch):
		gui.debug.Action("editor jump_prev_prompt")
		gui.jumpToPrompt(-1)

		return true
	case gui.matchesAction("jump_next_prompt", key, ch):
		gui.debug.Action("editor jump_next_prompt")
		gui.jumpToPrompt(1)

		return true

	case gui.matchesAction("copy_last_output", key, ch):
		gui.debug.Action("editor copy_last_output")
		gui.copyLastCommandOutput()

		return true
	}

	return false
}

// editOnSecondaryTab is editDuringScroll's counterpart for the perf and env
// tabs: the handful of keys that still mean something when the panel is
// showing a report rather than a screen.
//
// Everything it does not claim returns false and falls through to gocui's own
// keybinding dispatch, which is what keeps Tab and Ctrl-C working — and what
// makes "i", "v" and "/" quietly do nothing, since none of them has a binding
// outside this Editor.
func (gui *Gui) editOnSecondaryTab(view *gocui.View, key gocui.Key, ch rune) bool {
	_, rows := view.InnerSize()

	// Same reason as editDuringScroll's switch for the per-branch debug lines.
	switch {
	case key == gocui.KeyArrowLeft:
		gui.debug.Action("editor focus_sessions")
		_ = gui.focusSessionsPanel()

		return true

	case key == gocui.KeyPgup:
		gui.debug.Action("editor scroll page_up")
		gui.scrollBy(gui.pageStep(rows))

		return true
	case key == gocui.KeyPgdn:
		gui.debug.Action("editor scroll page_down")
		gui.scrollBy(-gui.pageStep(rows))

		return true
	case key == gocui.KeyCtrlU:
		gui.debug.Action("editor scroll half_page_up")
		gui.scrollBy(gui.halfPageStep(rows))

		return true
	case key == gocui.KeyCtrlD:
		gui.debug.Action("editor scroll half_page_down")
		gui.scrollBy(-gui.halfPageStep(rows))

		return true

	case gui.matchesAction("next_tab", key, ch):
		gui.debug.Action("editor next_tab")
		gui.switchTab(1)

		return true
	case gui.matchesAction("prev_tab", key, ch):
		gui.debug.Action("editor prev_tab")
		gui.switchTab(-1)

		return true

	case ch == 'q':
		// Same reasoning as editDuringScroll's own 'q': a printable-key global
		// binding never fires as a fallback while this view is Editable.
		gui.debug.Action("editor quit")
		gui.g.Update(func(*gocui.Gui) error { return gocui.ErrQuit })

		return true

	case gui.matchesAction("help", key, ch):
		gui.debug.Action("editor help")
		_ = gui.showHelp(gui.g, view)

		return true

	case gui.matchesAction("zoom", key, ch):
		gui.debug.Action("editor zoom")
		_ = gui.toggleZoom(gui.g, view)

		return true
	}

	return false
}

// writeToSelected sends translated bytes to whichever session is currently
// selected. No-op if the list is empty (nothing to type into).
func (gui *Gui) writeToSelected(b []byte) {
	sess := gui.selectedSession()
	if sess == nil {
		return
	}

	gui.writeToSession(sess, b)
}

// writeToSession is writeToSelected's addressable form, used directly by
// dispatchKey while broadcasting (each marked session, not just the
// selected one). No-op on an empty payload.
//
// Typing into a session whose shell has exited does nothing at all, which from
// the user's side is indistinguishable from a frozen application — so it is
// reported in the status bar instead.
//
// The status is what decides, not the error from Write: Session.Kill only
// closes the pty on its SIGKILL escalation path, and writing to a pty master
// whose slave has no process left still succeeds. Relying on the error would
// therefore only report the case where the shell had to be killed the hard way.
func (gui *Gui) writeToSession(sess *session.Session, b []byte) {
	if len(b) == 0 {
		return
	}

	if sess.Status() == session.StatusExited {
		_ = gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("input.session_exited", sess.Name(), sess.ExitCode())))

		return
	}

	if _, err := sess.Write(b); err != nil {
		_ = gui.reportSessionError(fmt.Errorf("session %s : %w", sess.Name(), err))
	}
}

// enterPassThrough arms pass-through mode: every subsequent key (bar the
// escape prefix) goes to the shell. Scroll always resets to live on entry —
// typing a command is pointless if you can't see its output.
func (gui *Gui) enterPassThrough() {
	// Handing the keyboard to a shell whose screen is not the one on display
	// would type into something the user cannot see. Guarded here rather than
	// only at each call site because there are three of them (the Editor, a
	// double-click, focusSelectedShell) and they must not be able to disagree.
	if gui.outputTab != tabTerminal {
		return
	}

	sess := gui.selectedSession()
	if sess == nil || sess.Status() == session.StatusExited {
		return
	}

	gui.passThroughActive = true
	// An Escape pressed during a previous pass-through must not pair up with
	// the first one pressed in this one.
	gui.lastEscAt = time.Time{}
	gui.onSelectionChanged() // resets scroll to live and restarts the render task
	gui.refreshChrome()
}

// exitPassThrough disarms pass-through mode, back to scroll/navigation. The
// render task is restarted so it picks up the new mode — that is what takes
// the terminal cursor back off the panel.
func (gui *Gui) exitPassThrough() {
	gui.passThroughActive = false
	gui.lastEscAt = time.Time{}
	gui.restartOutput()
	gui.refreshChrome()
}

// scrollBy adjusts the output scroll offset, clamped to the selected
// session's available scrollback, and restarts the render task so the new
// offset is picked up immediately rather than waiting for the next tick.
//
// It does nothing while the alternate screen is active: a full-screen
// application does not feed the scrollback, so there is no history to scroll
// back into, and the keys that would do it belong to that application anyway.
func (gui *Gui) scrollBy(delta int) {
	// The offset below addresses the session's scrollback, which the perf and
	// env tabs are not showing — they scroll their own rendered text instead,
	// through gocui's view origin.
	if gui.outputTab != tabTerminal {
		gui.scrollSecondaryTab(delta)

		return
	}

	sess := gui.selectedSession()
	if sess == nil || sess.Screen().IsAltScreen() {
		return
	}

	offset := gui.getScrollOffset() + delta
	if offset < 0 {
		offset = 0
	}

	if max := sess.Screen().ScrollbackLen(); offset > max {
		offset = max
	}

	gui.setScrollOffset(offset)
	gui.showOutput(sess)
}

// scrollSecondaryTab moves the perf/env tabs' own offset. The sign is flipped
// against scrollBy's: its delta counts *backwards* from a live bottom, while a
// view origin counts forwards from the top, so PgUp — positive there — has to
// decrease the origin here.
//
// Clamped against what is currently rendered, so a key held down does not run
// the offset off into a number the user then has to scroll all the way back
// through. Only ever called from gocui's goroutine, like everything that
// touches tabOffset.
func (gui *Gui) scrollSecondaryTab(delta int) {
	if gui.g == nil {
		return
	}

	view, err := gui.g.View(outputViewName)
	if err != nil {
		return
	}

	offset := gui.tabOffset - delta

	if max := maxTabOffset(view); offset > max {
		offset = max
	}

	if offset < 0 {
		offset = 0
	}

	if offset == gui.tabOffset {
		return
	}

	gui.tabOffset = offset
	gui.restartOutput()
}

// refreshChrome updates the status bar text and the active border colour to
// reflect the current pass-through state — the two indicators the roadmap
// asks for, so the user always knows whether q quits the app or goes to the
// shell.
func (gui *Gui) refreshChrome() {
	if gui.g == nil {
		return
	}

	if gui.passThroughActive {
		gui.g.SelFrameColor = gui.theme.PassThroughBorderColor
	} else {
		gui.g.SelFrameColor = gui.theme.ActiveBorderColor
	}

	if view, err := gui.g.View(statusViewName); err == nil {
		gui.renderStatus(view)
	}
}
