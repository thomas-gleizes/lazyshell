package gui

import (
	"fmt"
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
	sess := gui.selectedSession()
	if sess == nil || rows <= 0 {
		return
	}

	scrollbackLen := sess.Screen().ScrollbackLen()
	maxLine := scrollbackLen + rows - 1

	cursor := gui.copyCursorLine + delta
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
	text := sess.Screen().TextRange(from, to)

	if err := gui.copyToClipboard(text); err != nil {
		_ = gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("copymode.copy_failed", err)))
	} else {
		gui.lastError = ""
	}

	gui.restartOutput()
	gui.refreshChrome()
}
