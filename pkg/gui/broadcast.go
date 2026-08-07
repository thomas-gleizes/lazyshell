package gui

import (
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// toggleBroadcastMark marks or unmarks the selected session for broadcast
// (the "b" binding) — no-op if there is no session to mark. Marking by
// itself does nothing yet: broadcastArmed only turns true once 2 or more
// sessions carry a mark, which is what actually starts duplicating
// keystrokes (pkg/gui/input.go's dispatchKey).
func (gui *Gui) toggleBroadcastMark(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	if gui.broadcastMarks == nil {
		gui.broadcastMarks = make(map[string]bool)
	}

	if gui.broadcastMarks[sess.ID] {
		delete(gui.broadcastMarks, sess.ID)
	} else {
		gui.broadcastMarks[sess.ID] = true
	}

	return gui.renderSessionsPanel()
}

// broadcastArmed reports whether a keystroke sent while selectedID is the
// current session should be duplicated to every marked session, rather than
// just written to that one: true once selectedID itself carries a mark and
// at least one other session does too. A single mark alone is "about to
// broadcast", not broadcasting — sending to a set of one is meaningless.
func (gui *Gui) broadcastArmed(selectedID string) bool {
	return len(gui.broadcastMarks) > 1 && gui.broadcastMarks[selectedID]
}

// broadcastMarkedSessions returns every currently marked session, in the
// manager's own order — the actual fan-out list dispatchKey writes to once
// broadcastArmed is true. Sessions that exited or were killed since being
// marked simply drop out here, since they are no longer in gui.sessions.List().
func (gui *Gui) broadcastMarkedSessions() []*session.Session {
	if len(gui.broadcastMarks) == 0 {
		return nil
	}

	var marked []*session.Session
	for _, sess := range gui.sessions.List() {
		if gui.broadcastMarks[sess.ID] {
			marked = append(marked, sess)
		}
	}

	return marked
}
