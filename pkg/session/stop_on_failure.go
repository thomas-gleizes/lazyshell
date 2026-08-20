package session

import "time"

// DefaultStopOnFailurePollInterval is how often watchStopOnFailure polls a
// running session's Screen for its injected command's OSC 133 exit event.
// pkg/screen's OSC 133 state (osc133.go) is deliberately poll-only — there is
// no callback fired as the sequence is parsed — so a free-running ticker is
// the only way to react to a command failing while the shell underneath it
// stays alive.
const DefaultStopOnFailurePollInterval = 200 * time.Millisecond

// watchStopOnFailure polls sess.Screen().LastCommandExit() until either the
// session exits on its own, or the very first command-exit event this
// incarnation's Screen ever reports arrives. By construction, opts.Command is
// injected as the first thing typed into a freshly created shell (see
// newSession), and each incarnation gets a brand-new Screen — so that first
// event is always the injected command's own exit, never a later command the
// user types by hand in the same still-running shell. Once observed, there is
// nothing left for this goroutine to watch for, so it returns, whether or not
// it killed anything.
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
			code, hasCode, seq, ok := sess.Screen().LastCommandExit()
			if !ok || seq != 1 {
				continue // not yet, or already past, the injected command's own event
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
