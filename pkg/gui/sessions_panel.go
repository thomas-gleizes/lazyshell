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

		fmt.Fprintf(&b, "%-12s %-8s %6d  %s\n", sess.Name(), sess.Status(), pid, sess.Cwd)
	}

	return b.String()
}

// selectedSession returns the session at the current selection, clamped to
// the current list bounds (the list can shrink under us — a session can exit
// or be killed while it is selected), or nil if there is none.
func (gui *Gui) selectedSession() *session.Session {
	sessions := gui.sessions.List()
	if len(sessions) == 0 {
		return nil
	}

	index := gui.getSelectedIndex()
	if index >= len(sessions) {
		index = len(sessions) - 1
	}

	if index < 0 {
		index = 0
	}

	gui.setSelectedIndex(index)

	return sessions[index]
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
	selected := gui.getSelectedIndex()

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

		index := gui.getSelectedIndex() + delta
		if index < 0 {
			index = 0
		}
		if index >= len(sessions) {
			index = len(sessions) - 1
		}
		gui.setSelectedIndex(index)

		gui.onSelectionChanged()

		return gui.renderSessionsPanel()
	}
}

// onSelectionChanged starts rendering the newly selected session's output,
// replacing whichever session was being rendered before. Scroll always
// resets to live: an old scroll position from a different session would be
// meaningless here.
func (gui *Gui) onSelectionChanged() {
	gui.setScrollOffset(0)

	if sess := gui.selectedSession(); sess != nil {
		gui.showOutput(sess)
	}
}

// newSession creates a session running the user's shell, in lazyshell's own
// working directory, and selects it. A creation failure is shown in the
// status bar rather than propagated.
func (gui *Gui) newSession(*gocui.Gui, *gocui.View) error {
	gui.sessionCounter++
	name := fmt.Sprintf("session-%d", gui.sessionCounter)

	if _, err := gui.sessions.New(name, gui.defaultShell()); err != nil {
		return gui.reportSessionError(err)
	}

	gui.lastError = ""

	return gui.selectNewlyCreatedSession()
}

// killSession asks for confirmation before killing the selected session.
func (gui *Gui) killSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showConfirm(fmt.Sprintf("Tuer la session %q ? (y/n)", sess.Name()), func() error {
		return gui.sessions.Kill(sess.ID)
	})
}

// renameSession prompts for a new name, pre-filled with the current one — the
// "renommage de session" ergonomics feature. Purely cosmetic: SetName does
// not touch the running shell.
func (gui *Gui) renameSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showPrompt("renommer la session", sess.Name(), func(newName string) error {
		if newName == "" {
			return nil
		}

		sess.SetName(newName)

		return nil
	})
}

// duplicateSession immediately starts a new session with the same shell and
// working directory as the selected one — no prompt, unlike newSessionInDir,
// since there is nothing to ask the user.
func (gui *Gui) duplicateSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	if _, err := gui.sessions.NewInDir(sess.Name()+"-copie", gui.defaultShell(), sess.Cwd); err != nil {
		return gui.reportSessionError(err)
	}

	gui.lastError = ""

	return gui.selectNewlyCreatedSession()
}

// newSessionInDir prompts for a working directory, pre-filled with
// lazyshell's own cwd, and starts a session there — the "session dans un cwd
// choisi" ergonomics feature.
func (gui *Gui) newSessionInDir(*gocui.Gui, *gocui.View) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}

	return gui.showPrompt("nouvelle session dans...", cwd, func(dir string) error {
		if dir == "" {
			return nil
		}

		gui.sessionCounter++
		name := fmt.Sprintf("session-%d", gui.sessionCounter)

		if _, err := gui.sessions.NewInDir(name, gui.defaultShell(), dir); err != nil {
			return err
		}

		return gui.selectNewlyCreatedSession()
	})
}

// selectNewlyCreatedSession moves the selection to the last session in the
// list (creation always appends) and starts rendering its output — the
// common tail of newSession, duplicateSession and newSessionInDir.
func (gui *Gui) selectNewlyCreatedSession() error {
	gui.setSelectedIndex(len(gui.sessions.List()) - 1)
	gui.onSelectionChanged()

	return gui.renderSessionsPanel()
}

// reportSessionError shows err in the status bar in place of the keybinding
// hint, the same way every session-creation failure is surfaced.
func (gui *Gui) reportSessionError(err error) error {
	gui.lastError = err.Error()

	if view, verr := gui.g.View(statusViewName); verr == nil {
		gui.renderStatus(view)
	}

	return nil
}

// defaultShell resolves the shell to start new sessions with: pkg/config's
// Shell if set, else $SHELL, else /bin/bash — mirrors cmd/spike-pty's shell()
// helper, duplicated here because that command is package main and cannot be
// imported.
func (gui *Gui) defaultShell() string {
	if gui.configuredShell != "" {
		return gui.configuredShell
	}

	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	return "/bin/bash"
}
