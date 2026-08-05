package gui

import (
	"fmt"
	"os"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// sessionsPanelContent renders one line per session (name, status, PID, cwd).
// A pure function, kept separate from the gocui-writing side so it can be
// tested directly, the same way keys.Translate and spike.edit are.
func sessionsPanelContent(sessions []*session.Session) string {
	if len(sessions) == 0 {
		return "Aucune session — n pour en créer une"
	}

	var b strings.Builder
	for _, sess := range sessions {
		pid := 0
		if sess.Cmd.Process != nil {
			pid = sess.Cmd.Process.Pid
		}

		fmt.Fprintf(&b, "%-12s %-8s %6d  %s\n", sess.Name, sess.Status(), pid, sess.Cwd)
	}

	return b.String()
}

// selectedSession returns the session at selectedIndex, clamped to the
// current list bounds (the list can shrink under us — a session can exit or
// be killed while it is selected), or nil if there is none.
func (gui *Gui) selectedSession() *session.Session {
	sessions := gui.sessions.List()
	if len(sessions) == 0 {
		return nil
	}

	if gui.selectedIndex >= len(sessions) {
		gui.selectedIndex = len(sessions) - 1
	}

	if gui.selectedIndex < 0 {
		gui.selectedIndex = 0
	}

	return sessions[gui.selectedIndex]
}

// renderSessionsPanel redraws the session list and keeps the cursor on the
// selected line. Called on a ticker (statuses change asynchronously in the
// background) and after every action that changes the list or the selection.
//
// Scheduled through g.Update rather than written directly: View.Write/Clear
// are safe from any goroutine (an internal writeMutex), but View.SetCursor
// is a plain field write with no such protection, and goEvery drives this
// from its own background goroutine — mutating it outside g.Update would
// race against gocui's own render pass reading it during MainLoop.
func (gui *Gui) renderSessionsPanel() error {
	if gui.g == nil {
		return nil
	}

	content := sessionsPanelContent(gui.sessions.List())
	selected := gui.selectedIndex

	gui.g.Update(func(g *gocui.Gui) error {
		view, err := g.View(sessionsViewName)
		if err != nil {
			// Not created yet — the first layout pass has not run.
			return nil
		}

		view.Clear()
		fmt.Fprint(view, content)
		view.SetCursor(0, selected)

		return nil
	})

	return nil
}

// selectionMoved moves the selection by delta (j/k, arrows), clamped to the
// list bounds, and shows the newly selected session's output.
func (gui *Gui) selectionMoved(delta int) func(*gocui.Gui, *gocui.View) error {
	return func(*gocui.Gui, *gocui.View) error {
		sessions := gui.sessions.List()
		if len(sessions) == 0 {
			return nil
		}

		gui.selectedIndex += delta
		if gui.selectedIndex < 0 {
			gui.selectedIndex = 0
		}
		if gui.selectedIndex >= len(sessions) {
			gui.selectedIndex = len(sessions) - 1
		}

		gui.onSelectionChanged()

		return gui.renderSessionsPanel()
	}
}

// onSelectionChanged starts rendering the newly selected session's output,
// replacing whichever session was being rendered before.
func (gui *Gui) onSelectionChanged() {
	if sess := gui.selectedSession(); sess != nil {
		gui.showOutput(sess)
	}
}

// newSession creates a session running the user's shell and selects it. A
// creation failure is shown in the status bar rather than propagated — there
// is no error popup before phase 5.
func (gui *Gui) newSession(*gocui.Gui, *gocui.View) error {
	gui.sessionCounter++
	name := fmt.Sprintf("session-%d", gui.sessionCounter)

	if _, err := gui.sessions.New(name, defaultShell()); err != nil {
		gui.lastError = err.Error()

		if view, verr := gui.g.View(statusViewName); verr == nil {
			gui.renderStatus(view)
		}

		return nil
	}

	gui.lastError = ""
	gui.selectedIndex = len(gui.sessions.List()) - 1
	gui.onSelectionChanged()

	return gui.renderSessionsPanel()
}

// killSession asks for confirmation before killing the selected session.
func (gui *Gui) killSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showConfirm(fmt.Sprintf("Tuer la session %q ? (y/n)", sess.Name), func() error {
		return gui.sessions.Kill(sess.ID)
	})
}

// defaultShell mirrors cmd/spike-pty's shell() helper — duplicated here
// because that command is package main and cannot be imported.
func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	return "/bin/bash"
}
