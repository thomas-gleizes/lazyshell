package gui

import (
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

// workingAgentSessionNames returns the display names of every session whose
// AI agent is mid-turn (agent.StateWorking), in Manager.List order. Quitting
// lazyshell tears down every pty along with whatever it is running, so this
// is what quit checks before doing that silently — StateBlocked and
// StateDone are excluded on purpose: both are already waiting on the user,
// so nothing in-flight would be lost.
func (gui *Gui) workingAgentSessionNames() []string {
	var names []string
	for _, sess := range gui.sessions.List() {
		if sess.AgentState() == agent.StateWorking {
			names = append(names, sess.Name())
		}
	}

	return names
}

// quitConfirmMessage reports whether quitting right now needs confirmation
// and, if so, the message to show for it. Shared by quit and
// requestQuitFromEditor so the two call sites the app can quit from — the
// global "q"/Ctrl-C binding and the Editable views' hand-matched "q"
// fallback (see input.go) — agree on when a working agent blocks a silent
// quit.
func (gui *Gui) quitConfirmMessage() (string, bool) {
	names := gui.workingAgentSessionNames()
	if len(names) == 0 {
		return "", false
	}

	return gui.tr.T("sessions.quit_confirm", strings.Join(names, ", ")), true
}

// quit is the "q"/Ctrl-C handler (see staticBindings). It quits outright
// unless an agent is mid-turn, in which case it asks for confirmation first
// — see quitConfirmMessage.
func (gui *Gui) quit(*gocui.Gui, *gocui.View) error {
	if msg, ok := gui.quitConfirmMessage(); ok {
		return gui.showConfirm(msg, func() error { return gocui.ErrQuit })
	}

	return gocui.ErrQuit
}

// requestQuitFromEditor is quit's counterpart for input.go's two Editor-mode
// "q" fallbacks (scroll mode, search): Editor.Edit returns a bool, not an
// error, so unlike quit it cannot hand gocui.ErrQuit back to gocui directly
// and instead injects it via g.Update, the same way those call sites already
// did before this existed.
func (gui *Gui) requestQuitFromEditor() {
	if msg, ok := gui.quitConfirmMessage(); ok {
		_ = gui.showConfirm(msg, func() error { return gocui.ErrQuit })

		return
	}

	gui.g.Update(func(*gocui.Gui) error { return gocui.ErrQuit })
}
