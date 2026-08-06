package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
)

// searchActive reports whether a scrollback search currently has matches to
// cycle through. A submitted pattern with zero matches does *not* count as
// active — see onSearchSubmit — so the footer/status/n/N wiring only lights
// up once there is somewhere to jump.
func (gui *Gui) searchActive() bool {
	return gui.searchPattern != "" && len(gui.searchMatches) > 0
}

// showSearch opens the pattern-entry popup, pre-filled with the last search
// if there was one. No-op if no session is selected: there would be nothing
// to search.
func (gui *Gui) showSearch(_ *gocui.Gui, _ *gocui.View) error {
	if gui.selectedSession() == nil {
		return nil
	}

	return gui.showPrompt(gui.tr.T("search.title"), gui.searchPattern, gui.onSearchSubmit)
}

// onSearchSubmit is showSearch's onSubmit: called with the popup's trimmed
// text on Enter. An empty submission clears the search, same as Esc. A
// pattern with no matches returns an error, which submitPrompt shows in the
// status bar — that is deliberately what stops searchActive from turning
// on: a search state with zero matches would have nothing for n/N to do and
// nothing to highlight, indistinguishable from no search at all except for
// the leftover status text.
func (gui *Gui) onSearchSubmit(text string) error {
	if text == "" {
		gui.clearSearch()

		return nil
	}

	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	matches := sess.Screen().Find(text)
	if len(matches) == 0 {
		gui.clearSearch()

		return fmt.Errorf("%s", gui.tr.T("search.no_matches", text))
	}

	gui.searchPattern = text
	gui.searchMatches = matches
	// The match closest to the live bottom is what a user typing a fresh
	// search is most likely looking for — the most recent occurrence.
	gui.searchIndex = len(matches) - 1
	gui.jumpToMatch()

	return nil
}

// nextMatch moves to the next (delta 1) or previous (delta -1) match,
// wrapping at either end, and scrolls to it. No-op while no search is
// active.
func (gui *Gui) nextMatch(delta int) {
	if !gui.searchActive() {
		return
	}

	n := len(gui.searchMatches)
	gui.searchIndex = ((gui.searchIndex+delta)%n + n) % n
	gui.jumpToMatch()
}

// jumpToMatch scrolls the output panel so the current match is at the top
// of the window and restarts the render task so the highlight and the new
// offset are picked up immediately, then refreshes the status bar's match
// counter.
func (gui *Gui) jumpToMatch() {
	sess := gui.selectedSession()
	if sess == nil {
		return
	}

	scr := sess.Screen()
	offset := scr.ScrollbackLen() - gui.searchMatches[gui.searchIndex]
	if offset < 0 {
		offset = 0
	} else if max := scr.ScrollbackLen(); offset > max {
		offset = max
	}

	gui.setScrollOffset(offset)
	gui.showOutput(sess)
	gui.refreshSearchStatus()
}

// clearSearch resets the search state — called on Esc, an empty
// resubmission, a failed search, and whenever the selected session changes
// (searchMatches' indices are only meaningful for the emulator they were
// found in).
func (gui *Gui) clearSearch() {
	gui.searchPattern = ""
	gui.searchMatches = nil
	gui.searchIndex = 0
}

// refreshSearchStatus redraws the status bar so a jump's new match count is
// visible immediately, the same pattern refreshChrome uses for the
// pass-through indicator.
func (gui *Gui) refreshSearchStatus() {
	if gui.g == nil {
		return
	}

	if view, err := gui.g.View(statusViewName); err == nil {
		gui.renderStatus(view)
	}
}
