package gui

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// Grouping is a *rendering* concern and nothing else — see ADR 0007. A group
// is a plain string on the session (session.Session.Group), the manager knows
// nothing about it, and its order in the list is recomputed from scratch on
// every tick. Nothing here mutates anything.

// groupOrderOf returns the groups present in sessions, in the order their
// headers must be drawn: the ones declared in the project file first, in
// declaration order, then any group that only exists at runtime — created
// with the "g" key or through `lazyshell ctl group` — in order of first
// appearance in sessions.
//
// Declaration order rather than alphabetical because the order of a
// lazyshell.yml's `groups:` block is its author saying what matters first;
// sorting it would throw that away. First-appearance for the rest because it
// is the only order available that is stable and needs no extra state.
//
// Only groups that actually have a session here are returned. sessions is the
// *visible* set, so a declared group whose sessions are all filtered out
// contributes no header rather than an empty box. The ungrouped sessions are
// not a group and never appear in this list.
func groupOrderOf(sessions []*session.Session, declared []string) []string {
	present := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if group := sess.Group(); group != "" {
			present[group] = true
		}
	}

	if len(present) == 0 {
		return nil
	}

	order := make([]string, 0, len(present))
	taken := make(map[string]bool, len(present))

	for _, group := range declared {
		if present[group] && !taken[group] {
			order = append(order, group)
			taken[group] = true
		}
	}

	for _, sess := range sessions {
		group := sess.Group()
		if group != "" && !taken[group] {
			order = append(order, group)
			taken[group] = true
		}
	}

	return order
}

// orderByGroup rearranges sessions into the order they are displayed in:
// group by group in groupOrderOf's order, ungrouped sessions last.
//
// Stable within a group — the manager's own order is preserved there, so a
// session never moves relative to its neighbours for any reason other than a
// group changing. Hence the partition below rather than a sort: a comparison
// sort would need a total order over groups anyway, and sort.Slice is not
// stable.
//
// Returns sessions untouched when nothing is grouped, which is the common
// case and must stay byte-for-byte what the panel rendered before groups
// existed.
func orderByGroup(sessions []*session.Session, declared []string) []*session.Session {
	groups := groupOrderOf(sessions, declared)
	if len(groups) == 0 {
		return sessions
	}

	ordered := make([]*session.Session, 0, len(sessions))

	for _, group := range groups {
		for _, sess := range sessions {
			if sess.Group() == group {
				ordered = append(ordered, sess)
			}
		}
	}

	for _, sess := range sessions {
		if sess.Group() == "" {
			ordered = append(ordered, sess)
		}
	}

	return ordered
}

// groupHeaderLine renders a group's header, terminator included: the label,
// then a rule filling the panel's width, so the eye can follow the boundary
// across a narrow column.
//
// Dimmed rather than colored: the gutter markers already own color in this
// panel (agent state), and a header competing with them would make a blocked
// agent harder to spot, which is the one thing the list exists to surface.
func groupHeaderLine(label string, width int) string {
	head := "── " + label + " "

	if pad := width - len([]rune(head)); pad > 0 {
		head += strings.Repeat("─", pad)
	}

	return "\x1b[2m" + head + "\x1b[0m\n"
}

// selectedGroup is the group the selection is in, "" when there is no
// selection or it is ungrouped. Every group action starts here: the group is
// addressed through the selected session rather than being a separate piece
// of state, so there is never a "current group" that disagrees with what the
// cursor is on.
func (gui *Gui) selectedGroup() string {
	sess := gui.selectedSession()
	if sess == nil {
		return ""
	}

	return sess.Group()
}

// groupSessions returns every session of group, in the manager's order.
// Reads the manager's full list, not the filtered one: a group action means
// "all of them", and a filter that happens to be active must not silently
// shrink what a kill or a broadcast reaches.
func (gui *Gui) groupSessions(group string) []*session.Session {
	if group == "" {
		return nil
	}

	var members []*session.Session
	for _, sess := range gui.sessions.List() {
		if sess.Group() == group {
			members = append(members, sess)
		}
	}

	return members
}

// killSessions kills every id and reports how many actually died, joining the
// failures rather than stopping at the first: a half-killed group is the worst
// outcome of the three.
//
// Concurrently, because each Kill waits for its own process group to be
// reaped, escalating to SIGKILL after KillTimeout — in sequence a group of six
// costs six of those, which busts pkg/control's 3 s client deadline outright
// (GroupKill) and leaves the spinner up for half a minute (the "X" key). The
// sessions are independent processes and Manager.Kill is safe to call
// concurrently for different ids, so the sequential loop bought nothing.
//
// Never call this on gocui's goroutine: it is exactly the slow work
// pkg/gui/control.go's header comment is about.
func (gui *Gui) killSessions(ids []string) (int, error) {
	errc := make(chan error, len(ids))

	var wg sync.WaitGroup

	for _, id := range ids {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errc <- gui.sessions.Kill(id)
		}()
	}

	wg.Wait()
	close(errc)

	killed := 0

	var errs []error

	for err := range errc {
		if err != nil {
			errs = append(errs, err)

			continue
		}

		killed++
	}

	return killed, errors.Join(errs...)
}

// setSessionGroup is the "g" key: move the selected session into a group,
// typed into a popup pre-filled with the one it is in. An empty submission
// takes it out of every group, which is the only way back to ungrouped and
// is why the prompt does not refuse a blank answer.
func (gui *Gui) setSessionGroup(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showPrompt(gui.tr.T("prompt.set_group"), sess.Group(), func(group string) error {
		group = strings.TrimSpace(group)

		// A newline cannot be typed into the single-line prompt, but a paste
		// can carry one, and it would tear the header line in two — the same
		// rule pkg/config enforces on the project file.
		group = strings.ReplaceAll(strings.ReplaceAll(group, "\n", " "), "\r", " ")

		sess.SetGroup(group)
		gui.debug.Event("session %s (%s) grouped into %q", sess.Name(), sess.ID, group)

		// The session almost certainly moved in display order, so the
		// selection is re-derived from its id — the same tool a filter change
		// uses, and for the same reason: the old index now points elsewhere.
		gui.reselectAfterFilterChange(sess)

		return gui.renderSessionsPanel()
	})
}

// toggleGroupFilter is the "G" key: narrow the list to the selected session's
// group, or clear that narrowing if it is already the one in force. One
// keystroke each way, which is why this is a field of its own rather than a
// "group:" syntax inside the text filter — that could only be set through the
// prompt.
func (gui *Gui) toggleGroupFilter(*gocui.Gui, *gocui.View) error {
	group := gui.selectedGroup()
	if group == "" {
		return nil
	}

	previous := gui.selectedSession()

	if gui.groupFilter == group {
		gui.groupFilter = ""
	} else {
		gui.groupFilter = group
	}

	gui.reselectAfterFilterChange(previous)

	return gui.renderSessionsPanel()
}

// toggleGroupBroadcast is the "A" key: mark every session of the selected
// session's group for broadcast, or unmark them all if they already are.
//
// It reuses broadcastMarks rather than introducing a "broadcasting to a
// group" mode. There is then one arming rule (broadcastArmed), one gutter
// glyph and one fan-out list, and pkg/gui/input.go's keystroke path stays
// completely unaware that groups exist.
//
// The marks are by session id, so a session that leaves the group afterwards
// stays marked. Surprising at first glance, and correct: the marks are what
// the user armed, and silently dropping one because an unrelated regrouping
// happened would be worse than a mark they can see in the gutter and clear.
func (gui *Gui) toggleGroupBroadcast(*gocui.Gui, *gocui.View) error {
	members := gui.groupSessions(gui.selectedGroup())
	if len(members) == 0 {
		return nil
	}

	if gui.broadcastMarks == nil {
		gui.broadcastMarks = make(map[string]bool)
	}

	allMarked := true
	for _, sess := range members {
		if !gui.broadcastMarks[sess.ID] {
			allMarked = false

			break
		}
	}

	for _, sess := range members {
		if allMarked {
			delete(gui.broadcastMarks, sess.ID)
		} else {
			gui.broadcastMarks[sess.ID] = true
		}
	}

	return gui.renderSessionsPanel()
}

// killGroup is the "X" key: kill every session of the selected session's
// group, after a confirmation naming the group and how many sessions it will
// take down.
func (gui *Gui) killGroup(*gocui.Gui, *gocui.View) error {
	group := gui.selectedGroup()

	members := gui.groupSessions(group)
	if len(members) == 0 {
		return nil
	}

	// Snapshotted here, on the GUI goroutine, before the spinner's own
	// goroutine starts: the list can change underneath it, and killing
	// whatever happens to be in the group by then is not what was confirmed.
	ids := make([]string, 0, len(members))
	for _, sess := range members {
		ids = append(ids, sess.ID)
	}

	return gui.showConfirm(gui.tr.T("sessions.kill_group_confirm", group, len(ids)), func() error {
		gui.debug.Event("group %q killed (%d sessions)", group, len(ids))

		// Same reason as killSession's spinner, multiplied: even killed
		// concurrently, the group is only as fast as its slowest member's
		// KillTimeout escalation, which is seconds of frozen interface if it
		// runs inline.
		return gui.runBusy(gui.tr.T("busy.kill_group", group), func() error {
			_, err := gui.killSessions(ids)

			return err
		})
	})
}

// restartGroup is the "W" key: restart every exited session of the selected
// session's group.
//
// Sessions still running are skipped rather than reported as failures. A
// group being part exited and part alive is the normal state — that is what
// restarting a group is for — and erroring on it would make the key useless
// exactly when it is wanted.
func (gui *Gui) restartGroup(*gocui.Gui, *gocui.View) error {
	group := gui.selectedGroup()
	if group == "" {
		return nil
	}

	var ids []string
	for _, sess := range gui.groupSessions(group) {
		if sess.Status() == session.StatusExited {
			ids = append(ids, sess.ID)
		}
	}

	if len(ids) == 0 {
		return gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("sessions.restart_group_none", group)))
	}

	gui.debug.Event("group %q restarted (%d sessions)", group, len(ids))

	return gui.runBusyThen(gui.tr.T("busy.restart_group", group), func() error {
		var errs []error
		for _, id := range ids {
			if _, err := gui.sessions.Restart(id); err != nil {
				errs = append(errs, err)
			}
		}

		return errors.Join(errs...)
	}, func(err error) error {
		if err != nil {
			return gui.reportSessionError(err)
		}

		// Deliberately no focusSelectedShell, unlike the single-session "R":
		// a batch restart must not hand the keyboard to an arbitrary member
		// of the group the user was not looking at.
		gui.onSelectionChanged()

		return gui.renderSessionsPanel()
	})
}
