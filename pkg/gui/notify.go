package gui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// checkAgentNotifications scans every session for a fresh transition into
// agent.StateBlocked or agent.StateDone and fires one notification per
// transition — never a repeat for a state that has not changed since the
// last check, and never for a session currently hidden by a filter: a
// filtered-out session going blocked is exactly the case this feature
// exists to catch.
//
// Meant to run on its own goEvery tick (gui.go's Run), separate from
// renderSessionsPanel's: it must fire even on a tick renderSessionsPanel
// skips via its own content-diffing, and it must never become something
// that function's perf-tested diff logic has to account for.
func (gui *Gui) checkAgentNotifications() error {
	for _, sess := range gui.sessions.List() {
		state := sess.AgentState()

		gui.mu.Lock()
		last, seen := gui.notifiedState[sess.ID]
		if gui.notifiedState == nil {
			gui.notifiedState = map[string]agent.State{}
		}
		gui.notifiedState[sess.ID] = state
		gui.mu.Unlock()

		if seen && last == state {
			continue
		}

		gui.debug.Event("agent state %s: %v → %v", sess.Name(), last, state)

		if state == agent.StateBlocked || state == agent.StateDone {
			gui.notifyAgentState(sess, state)
		}
	}

	return nil
}

// notifyAgentState fires the desktop notification for sess entering state —
// OSC 9 and OSC 777 to the host terminal, or the configured fallback
// command instead, exactly the same either/or shape as copyToClipboard
// (clipboard.go).
func (gui *Gui) notifyAgentState(sess *session.Session, state agent.State) {
	key := "notify.done"
	if state == agent.StateBlocked {
		key = "notify.blocked"
	}

	msg := gui.tr.T(key, sess.Name())

	if gui.notifyFallback != "" {
		// Its own goroutine, unlike runClipboardFallback's synchronous call:
		// that one is triggered by a keypress (a single goroutine, blocking
		// is fine), this one fires from the shared periodic tick and must
		// never stall it — an external notifier can be arbitrarily slow.
		go runNotifyFallback(gui.notifyFallback, msg)

		return
	}

	oscNotifyWrite(msg)
}

// oscNotifyWrite writes msg to the host terminal via OSC 9 and OSC 777 —
// both, unconditionally: an OSC code a terminal does not understand is
// simply ignored, so sending both reaches more terminals than guessing
// which one to send, at negligible cost. Same target as oscClipboardWrite
// (clipboard.go) and the same reasoning: os.Stdout is the real terminal
// gocui only ever draws over, never redirects.
func oscNotifyWrite(msg string) {
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]9;%s\x07", msg)
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]777;notify;lazyshell;%s\x07", msg)
}

// runNotifyFallback runs command through the shell with the notification
// text on its stdin — the same shape runClipboardFallback gives any other
// user-supplied command in this codebase.
func runNotifyFallback(command, text string) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = strings.NewReader(text)

	_ = cmd.Run()
}
