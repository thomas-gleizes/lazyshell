package gui

import (
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// filterActive reports whether a sessions-list filter is currently narrowing
// what filteredSessions returns — either kind. Covering both is what makes Esc
// (clearFilterKey) and the hasActiveFilter binding predicate apply to a group
// filter without either of them having to know it exists.
func (gui *Gui) filterActive() bool {
	return gui.filterPattern != "" || gui.groupFilter != ""
}

// filteredSessions is the visible subset of the manager's list, in the
// manager's own order: with no filter it is that list unchanged; with the text
// filter it is the sessions whose name or cwd contains the pattern
// (case-insensitive); with the group filter it is one group. The two compose
// as an AND, and this is the single place that definition lives.
//
// Navigation reads displaySessions(), not this — same set, but in the order it
// is drawn in. This function is the *predicate*, that one is the order.
func (gui *Gui) filteredSessions() []*session.Session {
	all := gui.sessions.List()

	if !gui.filterActive() {
		return all
	}

	lower := strings.ToLower(gui.filterPattern)

	filtered := make([]*session.Session, 0, len(all))
	for _, sess := range all {
		if gui.groupFilter != "" && sess.Group() != gui.groupFilter {
			continue
		}

		if gui.filterPattern != "" && !strings.Contains(strings.ToLower(sess.Name()+" "+sess.Cwd), lower) {
			continue
		}

		filtered = append(filtered, sess)
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

// clearFilter resets both filters, keeping the current selection by ID the
// same way onFilterSubmit does. Both, because Esc means "get me back to the
// unfiltered view" and having to press it twice for two kinds of narrowing
// the user thinks of as one would be a distinction without a purpose.
func (gui *Gui) clearFilter() {
	previous := gui.selectedSession()

	gui.filterPattern = ""
	gui.groupFilter = ""

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
	filtered := gui.displaySessions()

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
