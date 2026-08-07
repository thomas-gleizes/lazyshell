package gui

import (
	"fmt"
	"os"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/keys"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// defaultPrefixKey is the tmux-style escape prefix that leaves pass-through
// mode. Every other key reaches the shell while pass-through is active, so
// this is the only way back — overridable through LAZYSHELL_PREFIX, because a
// terminal multiplexer running above lazyshell may well eat Ctrl-B before
// lazyshell ever sees it. Ported from cmd/spike-pty/main.go, which validated
// this mechanism in phase 1; duplicated rather than imported since that
// command is package main.
const defaultPrefixKey = gocui.KeyCtrlB

// prefixFrom resolves the pass-through escape prefix: $LAZYSHELL_PREFIX wins
// if set (a terminal multiplexer running above lazyshell may well eat Ctrl-B
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
	key, ch, mod = keys.Normalize(key, ch, mod)

	if gui.passThroughActive {
		return gui.editDuringPassThrough(key, ch, mod)
	}

	return gui.editDuringScroll(view, key, ch)
}

// editDuringPassThrough forwards everything to the selected session except
// the escape prefix: alone, it leaves pass-through; doubled, it sends one
// literal prefix byte (the same two-step automaton cmd/spike-pty validated).
func (gui *Gui) editDuringPassThrough(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if gui.prefixPending {
		gui.prefixPending = false

		if key == gui.prefixKey {
			gui.writeToSelected(gui.translate(key, ch, mod))
		} else {
			gui.exitPassThrough()
		}

		return true
	}

	if key == gui.prefixKey {
		gui.prefixPending = true

		return true
	}

	gui.writeToSelected(gui.translate(key, ch, mod))

	return true
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
	_, rows := view.Size()

	switch {
	// Copy-mode's own keys come first and win over everything below: while
	// selecting, j/k/arrows extend the selection instead of doing nothing,
	// and Esc cancels the selection rather than falling through to search's
	// Esc handler (searchActive() and copyModeActive are not expected to
	// overlap, but the order settles it either way).
	case gui.copyModeActive && key == gocui.KeyEsc && ch == 0:
		gui.cancelCopyMode()

		return true
	case gui.copyModeActive && (ch == 'y' || ch == 'v'):
		gui.yankCopySelection()

		return true
	case gui.copyModeActive && (ch == 'j' || key == gocui.KeyArrowDown):
		gui.moveCopyCursor(1, rows)

		return true
	case gui.copyModeActive && (ch == 'k' || key == gocui.KeyArrowUp):
		gui.moveCopyCursor(-1, rows)

		return true

	case (ch == 'i' || key == gocui.KeyEnter) && !gui.copyModeActive:
		gui.enterPassThrough()

		return true

	case ch == 'v' && !gui.copyModeActive:
		gui.enterCopyMode()

		return true

	case key == gocui.KeyPgup && !gui.copyModeActive:
		gui.scrollBy(gui.pageStep(rows))

		return true
	case key == gocui.KeyCtrlU && !gui.copyModeActive:
		gui.scrollBy(gui.halfPageStep(rows))

		return true

	case key == gocui.KeyPgdn && !gui.copyModeActive:
		gui.scrollBy(-gui.pageStep(rows))

		return true
	case key == gocui.KeyCtrlD && !gui.copyModeActive:
		gui.scrollBy(-gui.halfPageStep(rows))

		return true

	case ch == '/' && !gui.copyModeActive:
		_ = gui.showSearch(gui.g, view)

		return true

	case gui.searchActive() && ch == 'n':
		gui.nextMatch(1)

		return true
	case gui.searchActive() && ch == 'N':
		gui.nextMatch(-1)

		return true

	case gui.searchActive() && key == gocui.KeyEsc && ch == 0:
		gui.clearSearch()
		gui.restartOutput()
		gui.refreshSearchStatus()

		return true

	case ch == 'q':
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
		_ = gui.showHelp(gui.g, view)

		return true

	case gui.matchesAction("zoom", key, ch):
		// "zoom" is scoped to sessionsViewName, not global, but the same
		// problem applies: this view is Editable, so SetKeybinding is never
		// consulted for it regardless of scope. This is also the only way
		// back out of a zoomed output view — the sessions view that would
		// otherwise own this key does not exist while zoomed.
		_ = gui.toggleZoom(gui.g, view)

		return true
	}

	return false
}

// writeToSelected sends translated bytes to whichever session is currently
// selected. No-op if the list is empty (nothing to type into).
//
// Typing into a session whose shell has exited does nothing at all, which from
// the user's side is indistinguishable from a frozen application — so it is
// reported in the status bar instead.
//
// The status is what decides, not the error from Write: Session.Kill only
// closes the pty on its SIGKILL escalation path, and writing to a pty master
// whose slave has no process left still succeeds. Relying on the error would
// therefore only report the case where the shell had to be killed the hard way.
func (gui *Gui) writeToSelected(b []byte) {
	if len(b) == 0 {
		return
	}

	sess := gui.selectedSession()
	if sess == nil {
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
	if gui.selectedSession() == nil {
		return
	}

	gui.passThroughActive = true
	gui.onSelectionChanged() // resets scroll to live and restarts the render task
	gui.refreshChrome()
}

// exitPassThrough disarms pass-through mode, back to scroll/navigation. The
// render task is restarted so it picks up the new mode — that is what takes
// the terminal cursor back off the panel.
func (gui *Gui) exitPassThrough() {
	gui.passThroughActive = false
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
