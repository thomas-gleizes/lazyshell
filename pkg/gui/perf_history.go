package gui

import (
	"slices"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// The resources tab's state: the last sample of each session's processes, and
// the series behind its charts.
//
// It lives on Gui rather than in the render task's closure for two reasons.
// The first is that the closure is thrown away and rebuilt by every
// restartOutput — a scroll, a tab switch, a focus change — so a history kept
// there would reset the moment the user touched anything. The second is that
// sampling now runs in the background for *every* session, on its own tick,
// whether or not the tab is open: opening the resources tab shows a curve that
// is already there instead of one that starts from nothing.
//
// Concurrency: this is genuinely shared state. It is written only by
// samplePerf, on goEvery's background goroutine, and read only by the output
// render task, on its own. Both go through gui.mu — unlike the earlier version,
// where a single goroutine did both and no lock was needed.
//
// The render task must not rebuild its text on every 30 ms tick just because
// the panel redraws, so each history carries a version that samplePerf bumps.
// The renderer compares it against what it last drew and reuses its string
// otherwise; that is what keeps the tab as cheap as the others.

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

	// latest is the most recent sample, which is what the figures beside each
	// curve are printed from, and err why there is none. Kept here rather than
	// re-read by the renderer so that what is drawn and what is plotted always
	// come from the same instant.
	latest []session.ProcStats
	err    error

	// previous backs the CPU delta. It belongs to the sampler, not the
	// renderer, now that sampling happens in one place — see samplePerf.
	previous map[int]session.ProcStats

	// version is bumped on every sample. The renderer uses it to tell "nothing
	// has changed, reuse the text" from "resample happened, redraw".
	version int
}

// snapshot is what the renderer takes away from a history: a copy it can read
// without holding gui.mu, since rendering is not something to do under a lock
// the sampling goroutine also wants.
type perfSnapshot struct {
	version int

	latest []session.ProcStats
	err    error

	// shellCPU and the rest are copies, not the live slices: appendCapped
	// rewrites its backing array in place, so a renderer holding the original
	// would see it change under it mid-draw.
	shellCPU, shellRSS           []float64
	foregroundCPU, foregroundRSS []float64

	// chartIsForeground says which of the two the big chart should plot — the
	// foreground process when there is one, since that is what the user is
	// watching.
	chartIsForeground bool
}

func (h *perfHistory) snapshot() perfSnapshot {
	chart := h.chartTrack()

	return perfSnapshot{
		version:           h.version,
		latest:            slices.Clone(h.latest),
		err:               h.err,
		shellCPU:          slices.Clone(h.shell.cpu),
		shellRSS:          slices.Clone(h.shell.rss),
		foregroundCPU:     slices.Clone(h.foreground.cpu),
		foregroundRSS:     slices.Clone(h.foreground.rss),
		chartIsForeground: chart == &h.foreground,
	}
}

// cpu and rss pick a role's series out of a snapshot.
func (s perfSnapshot) cpu(foreground bool) []float64 {
	if foreground {
		return s.foregroundCPU
	}

	return s.shellCPU
}

func (s perfSnapshot) rss(foreground bool) []float64 {
	if foreground {
		return s.foregroundRSS
	}

	return s.shellRSS
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

// forgetPerfHistory drops a session's series, so a deleted session does not
// keep its samples alive for the rest of the process — and so an id that came
// round again would not inherit them.
//
// samplePerf prunes dead sessions on its own tick too; this is the immediate
// path, for the user explicitly removing one.
func (gui *Gui) forgetPerfHistory(sessionID string) {
	gui.mu.Lock()
	delete(gui.perfHistories, sessionID)
	gui.mu.Unlock()
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
