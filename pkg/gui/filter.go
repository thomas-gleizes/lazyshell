package gui

import (
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// filterActive reports whether a sessions-list filter is currently narrowing
// what filteredSessions returns.
func (gui *Gui) filterActive() bool {
	return gui.filterPattern != ""
}

// filteredSessions is what every part of the sessions panel that displays or
// navigates the list must read instead of gui.sessions.List() directly: with
// no filter it is that same list, unchanged; with one, it is the subset whose
// name or cwd contains the pattern (case-insensitive). This is the single
// place that definition lives, so "1"-"9", j/k and the rendered list can never
// disagree about what is currently selectable.
func (gui *Gui) filteredSessions() []*session.Session {
	all := gui.sessions.List()

	if !gui.filterActive() {
		return all
	}

	lower := strings.ToLower(gui.filterPattern)

	filtered := make([]*session.Session, 0, len(all))
	for _, sess := range all {
		if strings.Contains(strings.ToLower(sess.Name()+" "+sess.Cwd), lower) {
			filtered = append(filtered, sess)
		}
	}

	return filtered
}

// showFilter opens the pattern-entry popup, pre-filled with the current
// filter if there was one. No-op on an empty list: there would be nothing to
// filter.
func (gui *Gui) showFilter(_ *gocui.Gui, _ *gocui.View) error {
	if len(gui.sessions.List()) == 0 {
		return nil
	}

	return gui.showPrompt(gui.tr.T("filter.title"), gui.filterPattern, gui.onFilterSubmit)
}

// onFilterSubmit is showFilter's onSubmit: called with the popup's trimmed
// text on Enter. An empty submission clears the filter, same as Esc. Unlike
// search, a pattern with no matches is not an error — an empty filtered list
// already renders its own "no sessions" hint (sessionsPanelContent), and
// there is nothing to jump to or highlight the way a search result would.
//
// The previously selected session is kept selected if the new filter still
// includes it — reselecting by ID rather than reusing the old numeric index,
// since the filtered list's length (and so what a given index points at) can
// change with every keystroke.
func (gui *Gui) onFilterSubmit(text string) error {
	previous := gui.selectedSession()

	gui.filterPattern = text

	gui.reselectAfterFilterChange(previous)

	return nil
}

// clearFilter resets the filter, keeping the current selection by ID the same
// way onFilterSubmit does.
func (gui *Gui) clearFilter() {
	previous := gui.selectedSession()

	gui.filterPattern = ""

	gui.reselectAfterFilterChange(previous)
}

// clearFilterKey is the sessions panel's Esc handler: clears an active
// filter, the same "Esc gets you back to the unfiltered view" convention the
// scrollback search uses on the output panel. A plain Binding rather than a
// hand-matched key like search's, because sessionsViewName is not Editable —
// gocui does consult SetKeybinding for it. No-op (falls through to whatever
// else Esc might mean here, currently nothing) when there is no filter to
// clear.
func (gui *Gui) clearFilterKey(*gocui.Gui, *gocui.View) error {
	if gui.filterActive() {
		gui.clearFilter()
	}

	return gui.renderSessionsPanel()
}

// reselectAfterFilterChange re-derives selectedIndex from previous's ID
// against the newly filtered list, falling back to index 0 if that session is
// no longer visible (or there was none) — never leaving selectedIndex
// pointing past the end of a list that just shrank.
func (gui *Gui) reselectAfterFilterChange(previous *session.Session) {
	filtered := gui.filteredSessions()

	index := 0
	if previous != nil {
		for i, sess := range filtered {
			if sess.ID == previous.ID {
				index = i

				break
			}
		}
	}

	gui.setSelectedIndex(index)
	gui.onSelectionChanged()
}
