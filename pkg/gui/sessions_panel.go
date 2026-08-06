package gui

import (
	"fmt"
	"os"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// Gutter markers, in the two columns every session line starts with. They are
// the only way to learn something about a session that is not the one on
// screen: what it is running, and whether it asked for attention while hidden.
const (
	// bellMarker flags a session that emitted a BEL since it was last looked
	// at — a finished build, a shell asking a question.
	bellMarker = "!"
	// altScreenMarker flags a session with a full-screen application in
	// control (vim, htop, less).
	altScreenMarker = "#"
)

// sessionsPanelContent renders one line per session: a two-column gutter of
// markers, then name, status, PID, and either the terminal title the shell set
// (usually the running command, which is what you actually want to read in a
// session list) or the working directory when it set none.
//
// Exactly one line per session is a hard constraint, not a style choice:
// renderSessionsPanel's view.SetCursor(0, selected) and the view's Highlight
// both address sessions by line number.
//
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

		detail := sess.Screen().Title()
		if detail == "" {
			detail = sess.Cwd
		}

		fmt.Fprintf(&b, "%-2s%-12s %-8s %6d  %s\n", sessionMarkers(sess), sess.Name(), sess.Status(), pid, detail)
	}

	return b.String()
}

// sessionMarkers builds a session's gutter, at most two characters wide.
func sessionMarkers(sess *session.Session) string {
	markers := ""

	if sess.Screen().BellPending() {
		markers += bellMarker
	}

	if sess.Screen().IsAltScreen() {
		markers += altScreenMarker
	}

	return markers
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
//
// An unchanged panel is not pushed at all: a g.Update redraws the whole
// screen, and this runs on a 30 ms ticker whether or not any session status,
// title or marker has moved.
func (gui *Gui) renderSessionsPanel() error {
	if gui.g == nil {
		return nil
	}

	content := sessionsPanelContent(gui.sessions.List())
	selected := gui.getSelectedIndex()

	if !gui.sessionsPanelChanged(content, selected) {
		return nil
	}

	gui.g.Update(func(g *gocui.Gui) error {
		view, err := g.View(sessionsViewName)
		if err != nil {
			// Not created yet — the first layout pass has not run. Forget what
			// we just recorded, so the next tick draws instead of comparing
			// against something that was never displayed.
			gui.invalidateSessionsPanel()

			return nil
		}

		view.Clear()
		fmt.Fprint(view, content)
		view.SetCursor(0, selected)

		return nil
	})

	return nil
}

// sessionsPanelChanged reports whether the panel differs from what was last
// pushed, and records the new state as pushed. Guarded by gui.mu because
// renderSessionsPanel is called both from goEvery's background goroutine and
// from keybinding handlers on gocui's.
func (gui *Gui) sessionsPanelChanged(content string, selected int) bool {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.sessionsDrawn && content == gui.lastSessionsContent && selected == gui.lastSessionsSelected {
		return false
	}

	gui.lastSessionsContent, gui.lastSessionsSelected, gui.sessionsDrawn = content, selected, true

	return true
}

// invalidateSessionsPanel forgets what was last pushed, so the next render
// draws unconditionally.
func (gui *Gui) invalidateSessionsPanel() {
	gui.mu.Lock()
	gui.sessionsDrawn = false
	gui.mu.Unlock()
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
		// Looking at a session is what acknowledges its bell: the marker is
		// there to say "this one rang while you were elsewhere".
		sess.Screen().ClearBell()
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
