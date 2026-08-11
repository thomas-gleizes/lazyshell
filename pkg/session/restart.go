package session

import "time"

// RestartPolicy is a session's automatic restart-on-exit policy. RestartNever
// is the empty string deliberately — Go's zero value, so every Options{}
// literal that never mentions Restart keeps meaning "never" with no explicit
// opt-out required.
type RestartPolicy string

const (
	RestartNever     RestartPolicy = ""
	RestartOnFailure RestartPolicy = "on-failure"
	RestartAlways    RestartPolicy = "always"
)

const (
	// DefaultRestartBackoffBase/DefaultRestartBackoffMax bound the delay
	// before an automatic restart: it doubles each consecutive attempt,
	// starting at Base, capped at Max. There is deliberately no cap on the
	// number of attempts — the slowdown itself is the safeguard against an
	// instantly-failing command looping, not an attempt ceiling that would
	// eventually leave the session dead for good with no further action.
	DefaultRestartBackoffBase = 1 * time.Second
	DefaultRestartBackoffMax  = 60 * time.Second

	// DefaultRestartSuccessDuration is how long a restarted run must stay up
	// before its consecutive-attempt count resets to zero. Applies to every
	// automatic restart uniformly, not just ones that followed a failure —
	// an always-policy session exiting cleanly in a fast loop still needs to
	// be throttled, or it spins with no backoff at all.
	DefaultRestartSuccessDuration = 10 * time.Second
)

// WillAutoRestart reports whether this session, having exited, has an
// automatic restart pending. A pure function of already-race-free state, so
// it can be re-evaluated at any point without depending on timing relative
// to Manager's own bookkeeping — see fireAutoRestart, which relies on that.
//
// Deliberately not a Status value: every existing == StatusExited gate
// (ctl's wire format, restartGroup, restartSession, Manager.Restart) keeps
// working unchanged, and "R"/"W" get a free win as an immediate manual
// override of a pending backoff wait.
func (s *Session) WillAutoRestart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status != StatusExited || s.killedExplicitly {
		return false
	}

	switch s.opts.Restart {
	case RestartAlways:
		return true
	case RestartOnFailure:
		return s.exitCode != 0
	default:
		return false
	}
}

// RestartAttempts is how many consecutive automatic restarts led to this
// particular incarnation — a display value, stamped once at construction by
// Manager.newSession. It does not update itself as this *Session*'s own
// backoff state changes; a new attempt always means a new *Session*.
func (s *Session) RestartAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.restartAttempts
}

// setRestartAttempts is Manager.newSession's write into the field
// RestartAttempts reads. Unexported: nothing outside construction ever
// updates a live Session's own count.
func (s *Session) setRestartAttempts(n int) {
	s.mu.Lock()
	s.restartAttempts = n
	s.mu.Unlock()
}

// restartState is a Manager's per-id automatic-restart bookkeeping, guarded
// by the Manager's own mu rather than a lock of its own — every critical
// section that touches it also needs m.sessions examined in the same
// breath, and a second lock would only invite ordering bugs for no benefit.
type restartState struct {
	attempts int
	timer    *time.Timer
}

// backoffDelay is the pure exponential-backoff calculation behind supervise:
// base, doubled once per attempt already made, capped at maxDelay.
func backoffDelay(base, maxDelay time.Duration, attempts int) time.Duration {
	delay := base
	for i := 0; i < attempts && delay < maxDelay; i++ {
		delay *= 2
	}

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// supervise watches one session incarnation from creation to exit, and — if
// its policy calls for it — arms a backoff-delayed automatic restart.
// Started once per incarnation by newSession, never re-armed on the same
// *Session*: a restart, manual or automatic, always produces a fresh one,
// which gets its own supervise goroutine.
//
// It also runs the success-duration timer that resets the attempt count:
// the two concerns share this goroutine's lifetime because both need to stop
// cleanly the moment the session exits, not linger past it.
func (m *Manager) supervise(id string, sess *Session) {
	successTimer := time.AfterFunc(m.RestartSuccessDuration, func() {
		m.mu.Lock()
		if m.sessions[id] == sess {
			if st := m.restarts[id]; st != nil {
				st.attempts = 0
			}
		}
		m.mu.Unlock()
	})

	<-sess.Done()
	successTimer.Stop()

	if !sess.WillAutoRestart() {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Superseded already (a manual restart or removal raced this exit) —
	// nothing to arm for an incarnation that is no longer the live one.
	if m.sessions[id] != sess {
		return
	}

	st, ok := m.restarts[id]
	if !ok {
		st = &restartState{}
		m.restarts[id] = st
	}

	delay := backoffDelay(m.RestartBackoffBase, m.RestartBackoffMax, st.attempts)
	st.attempts++
	st.timer = time.AfterFunc(delay, func() { m.fireAutoRestart(id, sess) })
}

// fireAutoRestart is the backoff timer's callback. It re-derives correctness
// from ground truth rather than trusting the state supervise armed it with —
// this re-check is what makes a concurrent manual restart/kill/remove safe
// without a shared cancellation token: WillAutoRestart and the m.sessions
// identity check are both idempotent once sess.Done() has closed, so
// re-evaluating them here closes every timing window between "armed" and
// "fires". cancelSupervision, called from the manual paths, is only an
// optimization on top of this — stopping a wakeup that would otherwise do
// nothing — not the correctness mechanism itself.
func (m *Manager) fireAutoRestart(id string, sess *Session) {
	m.mu.Lock()
	if st := m.restarts[id]; st != nil {
		st.timer = nil
	}
	m.mu.Unlock()

	if !sess.WillAutoRestart() {
		return
	}

	m.mu.RLock()
	stale := m.sessions[id] != sess
	m.mu.RUnlock()

	if stale {
		return
	}

	_, _ = m.autoRestart(id, sess)
}

// autoRestart is Manager.Restart's automatic sibling: the same spawn-and-swap,
// but it does not reset the attempt count — continuing the backoff sequence
// is the point of an automatic retry, unlike a manual one — and it trusts
// its caller (fireAutoRestart) to have already confirmed sess is exited and
// still the live incarnation for id.
func (m *Manager) autoRestart(id string, sess *Session) (*Session, error) {
	opts := sess.opts
	opts.Group = sess.Group()

	newSess, err := m.newSession(id, opts)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if m.sessions[id] == sess {
		m.sessions[id] = newSess
	}
	m.mu.Unlock()

	return newSess, nil
}

// cancelSupervision stops id's pending automatic-restart timer, if any, and
// optionally resets its attempt count. An optimization only: correctness
// does not depend on this running before fireAutoRestart's own timer fires —
// see fireAutoRestart's doc comment — this just avoids a wasted wakeup and,
// for a manual restart, gives the fresh attempt its clean slate promptly
// rather than on the next exit.
func (m *Manager) cancelSupervision(id string, resetAttempts bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.restarts[id]
	if !ok {
		return
	}

	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}

	if resetAttempts {
		st.attempts = 0
	}
}
