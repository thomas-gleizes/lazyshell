package session

import (
	"fmt"
	"slices"
	"time"
)

// ProcStats is one process's resource usage at one instant. Everything here
// is a *cumulative* or *instantaneous* reading, never a rate: a percentage
// needs two samples, and deciding how far apart they are is the caller's
// business, not this package's (see pkg/gui's perf tab, which keeps the
// previous sample in its render task's closure).
type ProcStats struct {
	// PID is the process this sample is about, and Comm its bare name.
	PID  int
	Comm string

	// Foreground marks the sample taken for the pty's foreground process
	// group leader rather than for the shell itself — the `claude` or `vim`
	// the user is actually looking at. False for the shell's own sample.
	Foreground bool

	// CPUTime is the cumulative user+system time the process has consumed
	// since it started. A percentage is the delta between two of these over
	// the wall-clock time between them.
	CPUTime time.Duration

	// RSSBytes is the resident set size: physical memory currently held.
	RSSBytes uint64

	// Threads is the process's thread count, or 0 where the platform does not
	// report one without cgo (darwin) — see ThreadsAvailable.
	Threads          int
	ThreadsAvailable bool

	// DiskRead/DiskWritten are cumulative bytes actually fetched from and sent
	// to storage. DiskIOAvailable is false when the platform cannot report
	// them at all (darwin, where they live behind libproc's proc_pid_rusage
	// and therefore behind cgo) or refuses to (a hardened Linux kernel
	// answering EACCES on /proc/<pid>/io). A false here means "we cannot
	// know", never "zero bytes".
	DiskRead, DiskWritten uint64
	DiskIOAvailable       bool

	// SampledAt is when the reading was taken, so a caller computing a rate
	// divides by the real elapsed time rather than by its own tick period.
	SampledAt time.Time
}

// Stats samples the session's resource usage: the shell process first, then
// the pty's foreground process group leader when that is a different process.
//
// Deliberately *not* the whole process tree. What the user wants to see is the
// `claude` or `npm` in the foreground, not the `bash` that spawned it, and the
// foreground pgid is one ioctl away (foregroundPGID). Walking the tree would
// mean scanning all of /proc — or spawning a `ps -e` — once per session per
// refresh, a cost this feature does not justify.
//
// An exited session has nothing to sample: /proc/<pid> is gone and the pid may
// already have been reused, so this errors rather than reporting a stranger's
// numbers.
func (s *Session) Stats() ([]ProcStats, error) {
	pids, err := s.statsPIDs()
	if err != nil {
		return nil, err
	}

	return s.assembleStats(pids, sampleProcs(pids))
}

// statsPIDs is what this session wants sampled: its shell first, then the pty's
// foreground process group leader when that is a different process.
//
// Split out from Stats so Manager.StatsAll can collect every session's pids and
// pay for a single sampleProcs — which on darwin is a single `ps` spawn rather
// than one per session.
func (s *Session) statsPIDs() ([]int, error) {
	if s.Status() == StatusExited {
		return nil, fmt.Errorf("session %s: exited, no stats", s.ID)
	}

	if s.Cmd == nil || s.Cmd.Process == nil {
		return nil, fmt.Errorf("session %s: no process", s.ID)
	}

	shellPID := s.Cmd.Process.Pid
	pids := []int{shellPID}

	// A foreground lookup failure is not fatal: it only means the second,
	// nice-to-have sample is missing. The shell's own numbers are still worth
	// showing, and this fails routinely for a fraction of a second while the
	// shell is between two foreground jobs.
	fgPID, err := foregroundPGID(s.ptmx)
	if err == nil && fgPID > 0 && fgPID != shellPID {
		pids = append(pids, fgPID)
	}

	return pids, nil
}

// assembleStats picks this session's pids out of a batch of samples, in the
// order statsPIDs asked for them, and marks which one is the foreground.
func (s *Session) assembleStats(pids []int, samples map[int]ProcStats) ([]ProcStats, error) {
	if len(pids) == 0 {
		return nil, fmt.Errorf("session %s: nothing to sample", s.ID)
	}

	shellPID := pids[0]

	out := make([]ProcStats, 0, len(pids))
	for _, pid := range pids {
		sample, ok := samples[pid]
		if !ok {
			// The process went away between the pgid lookup and the read —
			// normal for a short-lived foreground command, so it is skipped
			// rather than reported as an error.
			continue
		}

		sample.Foreground = pid != shellPID
		out = append(out, sample)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("session %s: could not sample pid %d", s.ID, shellPID)
	}

	return out, nil
}

// StatsAll samples every live session at once, keyed by session id. Sessions
// that cannot be sampled — exited, or whose process vanished — are simply
// absent from the result.
//
// One call, not one per session: on darwin each sampleProcs is a `ps` spawn,
// so sampling eight sessions separately would mean eight processes every
// interval instead of one. This is what makes background sampling (pkg/gui
// keeps a history whether or not the resources tab is open) affordable.
func (m *Manager) StatsAll() map[string][]ProcStats {
	sessions := m.List()

	// pidsBySession keeps each session's pids in statsPIDs order, since that
	// order is what says which one is the shell.
	pidsBySession := make(map[string][]int, len(sessions))

	var all []int

	for _, sess := range sessions {
		pids, err := sess.statsPIDs()
		if err != nil {
			continue
		}

		pidsBySession[sess.ID] = pids
		all = append(all, pids...)
	}

	if len(all) == 0 {
		return nil
	}

	samples := sampleProcs(all)

	out := make(map[string][]ProcStats, len(pidsBySession))

	for id, pids := range pidsBySession {
		stats, err := m.sessionByID(id).assembleStats(pids, samples)
		if err != nil {
			continue
		}

		out[id] = stats
	}

	return out
}

// sessionByID is StatsAll's lookup. Never nil for an id it just collected pids
// from — Remove would have to run in between, and even then assembleStats only
// reads the id for its error message.
func (m *Manager) sessionByID(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[id]
	if !ok {
		return &Session{ID: id}
	}

	return sess
}

// Env is the environment the session's shell was started with — exactly what
// buildEnv produced (pkg/session/manager.go), in the same "KEY=value" shape as
// os.Environ.
//
// This is the *launch-time* environment, not the shell's current one: an
// `export` typed at the prompt after startup is not reflected here. Reading
// the live environment of a running child is Linux-only in practice
// (/proc/<pid>/environ; darwin's KERN_PROCARGS2 is undocumented and
// permission-restricted), and a value that silently means something different
// on each OS would be worse than one that always means the same thing.
//
// Cloned rather than returned directly: Cmd.Env is what the process was
// started with, and no caller has any business rewriting it.
func (s *Session) Env() []string {
	if s.Cmd == nil {
		return nil
	}

	return slices.Clone(s.Cmd.Env)
}
