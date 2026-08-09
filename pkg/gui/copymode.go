package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
)

// enterCopyMode arms copy-mode at the line currently at the top of the
// output panel's window — the vim-visual-mode model: a single key starts a
// one-line selection, movement extends it, the same key (or "y") yanks it.
// No-op if there is no session to select from.
func (gui *Gui) enterCopyMode() {
	sess := gui.selectedSession()
	// A full-screen application does not feed the scrollback (same reasoning
	// as scrollBy) — there would be nothing addressable to select.
	if sess == nil || sess.Screen().IsAltScreen() {
		return
	}

	top := sess.Screen().ScrollbackLen() - gui.getScrollOffset()

	gui.copyModeActive = true
	gui.copyAnchorLine = top
	gui.copyCursorLine = top

	gui.restartOutput()
	gui.refreshChrome()
}

// cancelCopyMode leaves copy-mode without copying anything — Esc's handler.
func (gui *Gui) cancelCopyMode() {
	gui.copyModeActive = false

	gui.restartOutput()
	gui.refreshChrome()
}

// copySelectionRange is the anchor/cursor pair, sorted low to high — the
// range every consumer (rendering, yanking) actually needs, since the user
// can extend a selection either up or down from where they started it.
func (gui *Gui) copySelectionRange() (from, to int) {
	from, to = gui.copyAnchorLine, gui.copyCursorLine
	if from > to {
		from, to = to, from
	}

	return from, to
}

// moveCopyCursor extends the selection by moving its live end delta lines
// (j/k, arrows), clamped to the session's addressable range, and scrolls the
// output panel just enough to keep the cursor inside the visible window —
// the same "just enough" a real pager scrolls by, rather than always
// recentering.
func (gui *Gui) moveCopyCursor(delta, rows int) {
	gui.setCopyCursor(gui.copyCursorLine+delta, rows)
}

// setCopyCursor is moveCopyCursor's absolute form: it puts the selection's
// live end on that scrollback line rather than delta lines from where it was.
// Split out for the mouse (pkg/gui/mouse.go), which knows the line it is
// pointing at and not a distance — the clamping and the "scroll just enough"
// adjustment below are exactly the same either way, and duplicating them is
// how the two would drift apart.
func (gui *Gui) setCopyCursor(line, rows int) {
	sess := gui.selectedSession()
	if sess == nil || rows <= 0 {
		return
	}

	scrollbackLen := sess.Screen().ScrollbackLen()
	maxLine := scrollbackLen + rows - 1

	cursor := line
	if cursor < 0 {
		cursor = 0
	} else if cursor > maxLine {
		cursor = maxLine
	}
	gui.copyCursorLine = cursor

	offset := gui.getScrollOffset()
	top := scrollbackLen - offset
	bottom := top + rows - 1

	switch {
	case cursor < top:
		offset = scrollbackLen - cursor
	case cursor > bottom:
		offset = scrollbackLen - (cursor - rows + 1)
	}

	if offset < 0 {
		offset = 0
	} else if offset > scrollbackLen {
		offset = scrollbackLen
	}

	gui.setScrollOffset(offset)
	gui.restartOutput()
}

// yankCopySelection copies the selected range's plain text to the clipboard
// (pkg/gui/clipboard.go) and leaves copy-mode, whether or not the copy
// succeeded — a clipboard failure is reported in the status bar, not by
// getting stuck in a mode with no visible way out.
func (gui *Gui) yankCopySelection() {
	sess := gui.selectedSession()
	gui.copyModeActive = false

	if sess == nil {
		gui.restartOutput()
		gui.refreshChrome()

		return
	}

	from, to := gui.copySelectionRange()
	gui.copyText(sess.Screen().TextRange(from, to))

	gui.restartOutput()
	gui.refreshChrome()
}

// copyText is yankCopySelection's and copyLastCommandOutput's shared
// dispatch tail: OSC 52 to the host terminal, or the configured fallback
// command instead, with the same either/or shape gui.notifyAgentState uses
// for OSC 9/777. A failure is reported in the status bar, never by getting
// stuck — there is no mode here to get stuck in either way.
func (gui *Gui) copyText(text string) {
	report := func(err error) error {
		if err != nil {
			return gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("copymode.copy_failed", err)))
		}

		gui.lastError = ""
		gui.lastInfo = ""

		return nil
	}

	// OSC 52 is a single write to the host terminal — far too fast to be worth
	// a popup. The configured fallback is a whole process instead, which can
	// take as long as it likes (and hang outright, on a misconfigured one), so
	// only that branch goes behind the spinner.
	if gui.clipboardFallback == "" {
		_ = report(gui.copyToClipboard(text))

		return
	}

	_ = gui.runBusyThen(gui.tr.T("busy.clipboard"), func() error {
		return gui.copyToClipboard(text)
	}, func(err error) error {
		if err := report(err); err != nil {
			return err
		}

		return gui.refreshAfterBusy()
	})
}

// copyLastCommandOutput copies the most recently finished command's output
// (per OSC 133 shell integration) to the clipboard in one keystroke, without
// entering copy-mode's interactive line selection at all. Reports a status
// message rather than doing nothing when no command has finished yet, or its
// start has since scrolled out of the addressable scrollback.
func (gui *Gui) copyLastCommandOutput() {
	sess := gui.selectedSession()
	if sess == nil {
		return
	}

	from, to, ok := sess.Screen().LastCommandOutputRange()
	if !ok {
		_ = gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("copymode.no_last_command")))

		return
	}

	gui.copyText(sess.Screen().TextRange(from, to))
	gui.refreshChrome()
}

// copyLastCommandOutputAction is copyLastCommandOutput wrapped as a
// keybinding handler — see bindings() in keybindings.go, and editDuringScroll
// in input.go for the hand-matched path that reaches it while the output
// view is Editable.
func (gui *Gui) copyLastCommandOutputAction(*gocui.Gui, *gocui.View) error {
	gui.copyLastCommandOutput()

	return nil
}
