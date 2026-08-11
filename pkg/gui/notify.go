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

// checkCommandExitNotifications scans every non-agent session for a fresh
// OSC-133 command exit (per pkg/screen's LastCommandExit) and fires one
// notification per finished command that exited non-zero — never a repeat
// for the same command, keyed on seq rather than the exit code itself, since
// two consecutive failing commands can share a code and both deserve their
// own notification.
//
// Agent sessions (sess.AgentState() != agent.StateNone) are skipped
// entirely: they already get their own blocked/done notifications from
// checkAgentNotifications, and a manifest-detected agent with no hooks wired
// is still "a detected AI agent session" by this codebase's own definition
// (pkg/session's AgentState), so this checks the same accessor rather than
// only "has a hook ever fired".
//
// Runs on the same shared tick as checkAgentNotifications, for the same
// reason: it must fire even on a renderSessionsPanel tick skipped by that
// function's own content-diffing.
func (gui *Gui) checkCommandExitNotifications() error {
	for _, sess := range gui.sessions.List() {
		if sess.AgentState() != agent.StateNone {
			continue
		}

		code, hasCode, seq, ok := sess.Screen().LastCommandExit()
		if !ok {
			continue
		}

		gui.mu.Lock()
		last, seen := gui.notifiedCommandExit[sess.ID]
		if gui.notifiedCommandExit == nil {
			gui.notifiedCommandExit = map[string]int64{}
		}
		gui.notifiedCommandExit[sess.ID] = seq
		gui.mu.Unlock()

		if seen && last == seq {
			continue
		}

		if hasCode && code != 0 {
			gui.notifyCommandExit(sess, code)
		}
	}

	return nil
}

// notifyCommandExit fires the desktop notification for sess's last command
// having exited with code — same either/or shape (OSC, or the configured
// fallback command) as notifyAgentState.
func (gui *Gui) notifyCommandExit(sess *session.Session, code int) {
	msg := gui.tr.T("notify.command_failed", sess.Name(), code)

	if gui.notifyFallback != "" {
		go runNotifyFallback(gui.notifyFallback, msg)

		return
	}

	oscNotifyWrite(msg)
}

// checkWatchNotifications scans every session for a fresh WatchHit (per
// pkg/session's LastWatchHit) and fires one notification per notify-eligible
// hit — never a repeat for the same hit, keyed on seq rather than the match
// itself for the same reason checkCommandExitNotifications is: two
// consecutive matches of the same pattern still deserve their own
// notification, seq is what tells them apart.
//
// Runs on the same shared tick as the other two checks, for the same reason:
// it must fire even on a renderSessionsPanel tick skipped by that function's
// own content-diffing.
func (gui *Gui) checkWatchNotifications() error {
	for _, sess := range gui.sessions.List() {
		hit, ok := sess.LastWatchHit()
		if !ok {
			continue
		}

		gui.mu.Lock()
		last, seen := gui.notifiedWatch[sess.ID]
		if gui.notifiedWatch == nil {
			gui.notifiedWatch = map[string]uint64{}
		}
		gui.notifiedWatch[sess.ID] = hit.Seq
		gui.mu.Unlock()

		if seen && last == hit.Seq {
			continue
		}

		if hit.Notify {
			gui.notifyWatchHit(sess, hit)
		}
	}

	return nil
}

// notifyWatchHit fires the desktop notification for sess's watch hit — same
// either/or shape (OSC, or the configured fallback command) as
// notifyAgentState/notifyCommandExit.
func (gui *Gui) notifyWatchHit(sess *session.Session, hit session.WatchHit) {
	msg := gui.tr.T("notify.watch_match", sess.Name(), hit.Pattern, truncateWatchLine(hit.Line))

	if gui.notifyFallback != "" {
		go runNotifyFallback(gui.notifyFallback, msg)

		return
	}

	oscNotifyWrite(msg)
}

// watchLineNotifyMax bounds how much of a matched line notifyWatchHit
// includes in its message — a build log's offending line can be arbitrarily
// long, and an OSC payload (or a fallback command's argv, depending on what
// it does with stdin) is not the place for it.
const watchLineNotifyMax = 80

// truncateWatchLine shortens line to watchLineNotifyMax runes, marking the
// cut with "…" so the message never claims to be the whole line when it
// is not.
func truncateWatchLine(line string) string {
	r := []rune(line)
	if len(r) <= watchLineNotifyMax {
		return line
	}

	return string(r[:watchLineNotifyMax]) + "…"
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
