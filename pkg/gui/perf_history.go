package gui

// The resources tab's charts need something the sampler cannot hold: memory
// across render tasks. perfSampler lives in the render task's closure, and that
// closure is thrown away and rebuilt by every restartOutput — a scroll, a tab
// switch, a focus change. A history kept there would reset on each of those,
// so the curve would vanish the moment the user touched anything.
//
// It therefore lives on Gui, keyed by session id, and is handed to each new
// sampler by showOutput.
//
// Concurrency: the *map* is only ever touched from gocui's goroutine
// (perfHistoryFor, called by showOutput). The *contents* are only ever written
// from the render task's goroutine. Those never overlap — tasks.Manager stops
// the previous task synchronously before starting the next (see its
// TestNewTaskStopsThePrevious), which is the same guarantee showOutput already
// relies on for its previous-frame comparison. Hence no mutex on either.

// perfHistoryLen bounds each series. Wider than any real panel, so the charts
// are never short of points, and small enough that a few dozen sessions cost
// nothing: two series of 400 float64 per process is ~6 KiB per session.
const perfHistoryLen = 400

// perfTrack is one process's series through time.
type perfTrack struct {
	// pid is whose series this is. A change of pid resets the series rather
	// than continuing it: the foreground process changes whenever the user
	// runs a command, and drawing `vim`'s memory as a continuation of `ls`'s
	// would draw a cliff that never happened.
	pid int

	cpu []float64
	rss []float64
}

func (t *perfTrack) push(pid int, cpu, rss float64) {
	if pid != t.pid {
		t.pid, t.cpu, t.rss = pid, nil, nil
	}

	t.cpu = appendCapped(t.cpu, cpu)
	t.rss = appendCapped(t.rss, rss)
}

// appendCapped adds value, dropping the oldest entry once the series is full.
func appendCapped(series []float64, value float64) []float64 {
	if len(series) < perfHistoryLen {
		return append(series, value)
	}

	// copy rather than reslice: reslicing from index 1 on every push would walk
	// the backing array forward forever, so the allocation could never be
	// reused and the series would keep moving through memory.
	copy(series, series[1:])
	series[len(series)-1] = value

	return series
}

// perfHistory is one session's series, one per role.
//
// Keyed by role rather than by pid — two tracks, not a map that grows with
// every command the user runs. Each track handles its own pid changes.
type perfHistory struct {
	shell      perfTrack
	foreground perfTrack
}

func (h *perfHistory) track(foreground bool) *perfTrack {
	if foreground {
		return &h.foreground
	}

	return &h.shell
}

// dropForeground clears the foreground series, for when a sample comes back
// with no foreground process at all — the command finished and the shell is
// back at its prompt.
//
// Without this the series would go stale rather than empty, and chartTrack
// below would keep plotting a process that no longer exists: after a `sleep`
// returned, the big chart stayed on `sleep`'s flat zero line while the shell
// was visibly at 50% right underneath it.
func (h *perfHistory) dropForeground() {
	h.foreground = perfTrack{}
}

// chartTrack is the series the big chart plots: the foreground process when
// there is one, since that is what the user is actually watching, and the shell
// otherwise.
func (h *perfHistory) chartTrack() *perfTrack {
	if len(h.foreground.cpu) > 0 {
		return &h.foreground
	}

	return &h.shell
}

// perfHistoryFor returns the session's history, creating it on first use.
// Called from gocui's goroutine only — see this file's header.
func (gui *Gui) perfHistoryFor(sessionID string) *perfHistory {
	if gui.perfHistories == nil {
		gui.perfHistories = make(map[string]*perfHistory)
	}

	history, ok := gui.perfHistories[sessionID]
	if !ok {
		history = &perfHistory{}
		gui.perfHistories[sessionID] = history
	}

	return history
}

// forgetPerfHistory drops a session's series, so a deleted session does not
// keep its samples alive for the rest of the process.
func (gui *Gui) forgetPerfHistory(sessionID string) {
	delete(gui.perfHistories, sessionID)
}

// maxOf is the largest value in a series, or 0 for an empty one. The charts
// scale against this rather than against a fixed ceiling: a shell sitting at
// 2% of a CPU would be a flat line at the bottom of a 0–100 axis, which is the
// common case and the least useful thing to draw.
func maxOf(series []float64) float64 {
	max := 0.0

	for _, value := range series {
		if value > max {
			max = value
		}
	}

	return max
}

// minOf is maxOf's counterpart, for the range-scaled series. Returns 0 for an
// empty one, matching maxOf so a caller comparing the two gets a degenerate
// window rather than a nonsensical one.
func minOf(series []float64) float64 {
	if len(series) == 0 {
		return 0
	}

	min := series[0]

	for _, value := range series[1:] {
		if value < min {
			min = value
		}
	}

	return min
}
