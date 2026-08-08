package gui

import (
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// watchSelectedExit reacts to the selected session's shell exiting on its own
// — `exit`, Ctrl-D, or whatever it was running finishing — as opposed to the
// user killing it from the panel with "x".
//
// Without this the interface is left pointing at a dead session: still in
// pass-through (red border), every keystroke going to a pty nobody reads, and
// the only feedback a "session … exited" error the moment you type. So the
// interface backs out on its own: pass-through is disarmed and focus goes
// back to the sessions panel, on the very same session — it stays selected,
// exited but still listed, so "R" restarts it and "x"/"D" disposes of it.
//
// Driven from goEvery's ticker like the rest of the periodic checks: a
// session's exit is detected by Session.drain's own goroutine, and there is no
// event channel from there into the gui.
func (gui *Gui) watchSelectedExit() error {
	if gui.g == nil {
		return nil
	}

	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	if sess.Status() != session.StatusExited {
		// Re-arms the check for this session: Manager.Restart reuses the same
		// id, so a restarted session must be able to trigger this again when
		// it exits a second time.
		gui.clearExitHandled(sess.ID)

		return nil
	}

	if !gui.markExitHandled(sess.ID) {
		return nil
	}

	gui.debug.Event("session %s (%s) exited on its own", sess.Name(), sess.ID)

	gui.g.Update(gui.backOutOfExitedSession)

	return nil
}

// backOutOfExitedSession is watchSelectedExit's effect, split out because it
// must run on gocui's goroutine — the only one allowed to touch
// passThroughActive and the current view — and is therefore only ever reached
// through g.Update.
func (gui *Gui) backOutOfExitedSession(g *gocui.Gui) error {
	// Only when the output panel is the one focused: a confirm, help or prompt
	// popup being open means the user is in the middle of something else, and
	// stealing focus from under them would be worse than leaving a dead
	// session on screen. The sessions panel itself is already where we want to
	// land.
	current := g.CurrentView()
	if current == nil || current.Name() != outputViewName {
		return nil
	}

	// Reading a dead session's env or last resource figures is a legitimate
	// post-mortem — that is arguably when the perf tab is most useful. Only
	// the output tab has a reason to be left, since that is the one that just
	// became a frozen screen.
	if gui.outputTab != tabTerminal {
		return nil
	}

	if gui.passThroughActive {
		gui.exitPassThrough()
	}

	// Zoomed, the sessions panel does not exist to receive focus — leaving
	// pass-through is the whole of what can be done here, and "z" is still
	// bound inside editDuringScroll to get out.
	if gui.zoomed {
		return nil
	}

	_, err := g.SetCurrentView(sessionsViewName)

	return err
}

// markExitHandled records id as having had its exit reacted to, and reports
// whether that is news — false means watchSelectedExit already backed out for
// this session and must not do it again on every subsequent tick. Guarded by
// mu: called from goEvery's background goroutine.
func (gui *Gui) markExitHandled(id string) bool {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.exitHandledID == id {
		return false
	}

	gui.exitHandledID = id

	return true
}

// clearExitHandled forgets id, but only if it is the one currently recorded:
// selecting another session must not re-arm the check for the exited one we
// just backed out of, or coming back to it would bounce the focus a second
// time.
func (gui *Gui) clearExitHandled(id string) {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.exitHandledID == id {
		gui.exitHandledID = ""
	}
}
