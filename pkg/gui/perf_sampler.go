package gui

import (
	"fmt"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// samplePerf reads every live session's resource usage and folds it into the
// histories the resources tab draws from.
//
// It runs on its own goEvery tick, independently of whether that tab — or even
// that session — is on screen. That is the point: a curve is only worth looking
// at if it already goes back further than the moment you opened it. The cost is
// bounded by doing it for all sessions in a single StatsAll (one `ps` on
// darwin, not one per session) and by an interval an order of magnitude slower
// than the redraw.
//
// Sole writer of gui.perfHistories, under gui.mu; the output render task is its
// only reader. Nothing samples from the render path any more, which is what
// stops two goroutines racing to append to the same series.
func (gui *Gui) samplePerf() error {
	if gui.perfInterval() <= 0 {
		return nil
	}

	sessions := gui.sessions.List()
	if len(sessions) == 0 {
		return nil
	}

	// Outside the lock: StatsAll walks /proc or spawns a `ps`, and holding mu
	// across that would block every render tick for its duration.
	stats := gui.sessions.StatsAll()

	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.perfHistories == nil {
		gui.perfHistories = make(map[string]*perfHistory)
	}

	live := make(map[string]bool, len(sessions))

	for _, sess := range sessions {
		live[sess.ID] = true

		history := gui.perfHistories[sess.ID]
		if history == nil {
			history = &perfHistory{}
			gui.perfHistories[sess.ID] = history
		}

		history.apply(sess, stats[sess.ID])
	}

	// A session killed while the tab was elsewhere would otherwise keep its
	// series alive for the life of the process. deleteSession does this too,
	// for the case where the user removes one explicitly.
	for id := range gui.perfHistories {
		if !live[id] {
			delete(gui.perfHistories, id)
		}
	}

	return nil
}

// apply folds one sample into a session's history. Called with gui.mu held.
func (h *perfHistory) apply(sess *session.Session, stats []session.ProcStats) {
	h.version++

	if len(stats) == 0 {
		// Not an error worth clearing the curves over: a session between two
		// foreground jobs, or one whose shell just exited, still has a history
		// worth looking at. Only the figures go away.
		h.latest = nil
		h.err = fmt.Errorf("%s", sess.Name())
		h.previous = nil

		return
	}

	h.err = nil
	h.latest = stats

	sawForeground := false

	for _, sample := range stats {
		percent, _ := cpuPercent(h.previous[sample.PID], sample, sess.CreatedAt)
		h.track(sample.Foreground).push(sample.PID, percent, float64(sample.RSSBytes))

		sawForeground = sawForeground || sample.Foreground
	}

	if !sawForeground {
		h.dropForeground()
	}

	next := make(map[int]session.ProcStats, len(stats))
	for _, sample := range stats {
		next[sample.PID] = sample
	}

	h.previous = next
}

// cpuPercent turns two cumulative CPU readings into a rate.
//
// A percentage needs two samples. The first one for a process has none to
// compare against, and rather than show a blank it falls back to the average
// since the session started — a real number, but a different one, so instant
// reports which of the two this is and the panel says so.
//
// A zero-valued before (no previous sample at all) and a negative delta (a pid
// reused by another process, or a clock that went backwards) take the same
// fallback: both mean "these two readings are not about the same run".
func cpuPercent(before, now session.ProcStats, createdAt time.Time) (percent float64, instant bool) {
	if !before.SampledAt.IsZero() {
		elapsed := now.SampledAt.Sub(before.SampledAt)
		if elapsed > 0 {
			used := now.CPUTime - before.CPUTime
			if used >= 0 {
				return 100 * float64(used) / float64(elapsed), true
			}
		}
	}

	since := time.Since(createdAt)
	if since <= 0 {
		return 0, false
	}

	return 100 * float64(now.CPUTime) / float64(since), false
}

// perfSnapshotFor hands the render task a copy of a session's history, or ok
// false when nothing has been sampled yet. Called from gocui's goroutine.
func (gui *Gui) perfSnapshotFor(sessionID string) (perfSnapshot, bool) {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	history, ok := gui.perfHistories[sessionID]
	if !ok {
		return perfSnapshot{}, false
	}

	return history.snapshot(), true
}
