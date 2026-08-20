package session

import "time"

// DefaultStopOnFailurePollInterval is how often watchStopOnFailure polls a
// running session's Screen for its injected command's OSC 133 exit event.
// pkg/screen's OSC 133 state (osc133.go) is deliberately poll-only — there is
// no callback fired as the sequence is parsed — so a free-running ticker is
// the only way to react to a command failing while the shell underneath it
// stays alive.
const DefaultStopOnFailurePollInterval = 200 * time.Millisecond

// watchStopOnFailure polls sess.Screen() until either the session exits on
// its own, or the first D event that closes out a cycle which actually ran a
// command arrives. It is *not* simply the first D event this incarnation's
// Screen ever reports: a shell with OSC 133 integration wired up (zsh/bash's
// precmd) fires one before showing its very first prompt too, closing a
// cycle that never had a command in it at all — that spurious D always
// precedes the injected command's own, and reacting to it instead would mean
// this goroutine forever watches for an event that already happened without
// ever checking the real one. LastCommandOutputRange's ok tells the two
// apart: it is only true when the cycle a D closes was preceded by a real B
// or C, i.e. a command was actually submitted for execution — never true for
// the shell's own boilerplate opening cycle. By construction, opts.Command is
// injected as the first thing typed into a freshly created shell (see
// newSession), so the first D with a real output range is always the
// injected command's own exit, never a later command the user types by hand
// in the same still-running shell. Once observed, there is nothing left for
// this goroutine to watch for, so it returns, whether or not it killed
// anything.
//
// Spawned once per incarnation by newSession, alongside supervise — never
// re-armed on the same *Session*.
func (m *Manager) watchStopOnFailure(id string, sess *Session) {
	interval := m.StopOnFailurePollInterval
	if interval <= 0 {
		interval = DefaultStopOnFailurePollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-sess.Done():
			return
		case <-ticker.C:
			code, hasCode, _, ok := sess.Screen().LastCommandExit()
			if !ok {
				continue // no command has finished yet, injected or otherwise
			}

			if _, _, ranOK := sess.Screen().LastCommandOutputRange(); !ranOK {
				continue // the shell's own opening cycle, no command ran in it
			}

			if hasCode && code != 0 {
				m.mu.RLock()
				stale := m.sessions[id] != sess
				m.mu.RUnlock()

				// Best-effort, not airtight, same tolerance fireAutoRestart
				// already lives with (restart.go): sess cannot be replaced
				// while still running, so the only way this goes stale
				// between the check and Kill's own lookup is a narrow race
				// with the session exiting on its own at this exact moment.
				if !stale {
					_ = m.Kill(id)
				}
			}

			return // the one event this goroutine exists to observe has happened
		}
	}
}
