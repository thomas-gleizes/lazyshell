package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/i18n"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// perfSampler owns the perf tab's rendering between two ticks of the output
// render task.
//
// It exists because sampling and drawing happen at completely different rates:
// the panel redraws on the shared 30 ms tick like everything else, but reading
// a process's usage costs a `/proc` walk on Linux and a whole `ps` spawn on
// macOS, and a CPU percentage measured over 30 ms would be noise anyway. So a
// sample is taken at most once per interval and the rendered text is reused in
// between — where the render task's own "unchanged frame is not pushed" rule
// then stops it repainting the screen.
//
// It lives in the render task's closure and is therefore touched by exactly one
// goroutine, serially — the same reasoning that lets showOutput keep its
// previous-frame comparison there rather than on Gui.
type perfSampler struct {
	sess     *session.Session
	interval time.Duration

	// tr is read-only after construction and T is a map lookup, so calling it
	// from the task's goroutine is safe — unlike anything on Gui.
	tr *i18n.Catalog

	// previous is the last sample per pid, which is what turns a cumulative
	// CPU time into a percentage. Keyed by pid rather than by position: the
	// foreground process changes from one sample to the next, and comparing
	// "the second entry" against a different process's clock would invent
	// spikes out of nothing.
	previous map[int]session.ProcStats

	// history outlives this sampler — see pkg/gui/perf_history.go for why it
	// cannot live in the closure with everything else here.
	history *perfHistory

	// width is the panel's inner width, captured at task start. The charts are
	// drawn to fit it, so layout restarts this task when it changes (there is
	// no other way for a task to learn it has been resized).
	width int

	content string
	takenAt time.Time
}

// hist is the series store, allocated on first use. A sampler built without one
// must not take the render task down: this is the only goroutine that would
// notice, and losing the charts is a far better outcome than losing the panel.
func (p *perfSampler) hist() *perfHistory {
	if p.history == nil {
		p.history = &perfHistory{}
	}

	return p.history
}

// frame returns what the perf tab should show right now, sampling only if the
// interval has elapsed since the last one.
func (p *perfSampler) frame() outputFrame {
	if p.content == "" || time.Since(p.takenAt) >= p.interval {
		p.refresh()
	}

	return outputFrame{content: p.content}
}

// refresh takes a sample and renders it, keeping the old text on failure only
// if there is none yet — an error is worth showing, but not worth flickering
// over a single failed read.
func (p *perfSampler) refresh() {
	p.takenAt = time.Now()

	stats, err := p.sess.Stats()
	if err != nil {
		p.content = "  " + p.tr.T("perf.unavailable", err)
		p.previous = nil

		return
	}

	// The history is appended to *before* rendering, so this sample's own point
	// is the rightmost one on the charts rather than lagging a tick behind the
	// figure printed beside it.
	sawForeground := false

	for _, sample := range stats {
		percent, _ := p.cpuPercent(sample)
		p.hist().track(sample.Foreground).push(sample.PID, percent, float64(sample.RSSBytes))

		sawForeground = sawForeground || sample.Foreground
	}

	if !sawForeground {
		p.hist().dropForeground()
	}

	var b strings.Builder

	// The big chart goes first, above the per-process blocks: it is the one
	// thing on this tab worth looking at rather than reading, so it must not be
	// what falls off the bottom of a short panel.
	if chart := p.renderCPUChart(); chart != "" {
		b.WriteString(chart)
	}

	for i, sample := range stats {
		if i > 0 {
			b.WriteString("\n")
		}

		b.WriteString(p.renderProcess(sample))
	}

	p.content = b.String()

	next := make(map[int]session.ProcStats, len(stats))
	for _, sample := range stats {
		next[sample.PID] = sample
	}

	p.previous = next
}

// cpuChartHeight is how many character rows the big chart gets — four rows is
// sixteen braille dot rows, enough shape to read a spike from a plateau without
// taking a whole screenful.
const cpuChartHeight = 4

// perfChartMinWidth is the panel width below which the charts are dropped
// entirely. A chart narrower than this shows fewer seconds than the eye needs
// to see a trend, and the figures beside it are the better use of the columns.
const perfChartMinWidth = 40

// renderCPUChart draws the foreground process's CPU over time — or the shell's
// when nothing is in the foreground. Empty string when the panel is too narrow
// or there is not yet anything to plot.
func (p *perfSampler) renderCPUChart() string {
	if p.width < perfChartMinWidth {
		return ""
	}

	track := p.hist().chartTrack()
	if len(track.cpu) < 2 {
		// A single point is not a curve; showing one would be a flat line that
		// says nothing about a series that has not started yet.
		return ""
	}

	max := maxOf(track.cpu)
	if max <= 0 {
		return ""
	}

	// Two columns of margin on each side, matching the text blocks' indent.
	width := p.width - 4

	name := p.tr.T("perf.shell")
	if track == &p.hist().foreground {
		name = p.tr.T("perf.foreground")
	}

	lines := []string{"  " + p.tr.T("perf.cpu_chart", name, max)}
	for _, row := range brailleChart(track.cpu, max, width, cpuChartHeight) {
		lines = append(lines, "  "+row)
	}

	return strings.Join(lines, "\n") + "\n\n"
}

// renderProcess is one process's block: a heading, then one metric per line.
func (p *perfSampler) renderProcess(sample session.ProcStats) string {
	role := p.tr.T("perf.shell")
	if sample.Foreground {
		role = p.tr.T("perf.foreground")
	}

	track := p.hist().track(sample.Foreground)

	lines := []string{
		fmt.Sprintf("  %s — %s (pid %d)", role, sample.Comm, sample.PID),
		// CPU is scaled from zero (idle is genuinely the floor), memory on its
		// own range (a process hovering around 8.9 MiB has no meaningful zero
		// to be compared against) — see sparklineWindow.
		p.metricLine(p.tr.T("perf.cpu"), p.cpuText(sample), track.cpu, true),
		p.metricLine(p.tr.T("perf.rss"), formatBytes(sample.RSSBytes), track.rss, false),
	}

	threads := p.tr.T("perf.unknown")
	if sample.ThreadsAvailable {
		threads = fmt.Sprintf("%d", sample.Threads)
	}

	// No sparkline on these two: a thread count barely moves, and the disk
	// figures are cumulative totals that only ever go up — a curve of a
	// monotonic counter is a diagonal line that says nothing.
	lines = append(lines, p.metricLine(p.tr.T("perf.threads"), threads, nil, false))

	disk := p.tr.T("perf.unavailable_os")
	if sample.DiskIOAvailable {
		disk = p.tr.T("perf.disk_io", formatBytes(sample.DiskRead), formatBytes(sample.DiskWritten))
	}

	lines = append(lines, p.metricLine(p.tr.T("perf.disk"), disk, nil, false), "")

	return strings.Join(lines, "\n")
}

// metricLabelWidth and metricValueWidth are the two fixed columns of a metric
// line. Fixed so that every sparkline on the tab starts at the same column and
// the curves can be compared down the panel rather than each floating wherever
// its own figure happened to end.
const (
	metricIndent      = 4
	metricLabelWidth  = 10
	metricValueWidth  = 12
	metricSparkColumn = metricIndent + metricLabelWidth + 1 + metricValueWidth + 1
)

// metricLine is one "label value ▁▂▃" row. series nil, or a panel too narrow
// for a sparkline worth drawing, gives just the label and the value.
func (p *perfSampler) metricLine(label, value string, series []float64, fromZero bool) string {
	line := fmt.Sprintf("%*s%-*s %-*s",
		metricIndent, "", metricLabelWidth, label, metricValueWidth, value)

	sparkWidth := p.width - metricSparkColumn
	if series == nil || p.width < perfChartMinWidth || sparkWidth < 8 {
		return strings.TrimRight(line, " ")
	}

	lo, hi := sparklineWindow(series, fromZero)

	spark := sparkline(series, lo, hi, sparkWidth)
	if strings.TrimSpace(spark) == "" {
		return strings.TrimRight(line, " ")
	}

	return line + " " + spark
}

// cpuText turns a cumulative CPU time into a percentage.
//
// A percentage is a rate, so it needs two samples. The first one after the tab
// is opened has none to compare against, and rather than show a blank it falls
// back to the process's average since it started — a real number, but a
// different one, so it is labelled as such instead of quietly passing for the
// instantaneous figure.
func (p *perfSampler) cpuText(sample session.ProcStats) string {
	percent, instant := p.cpuPercent(sample)

	if instant {
		return fmt.Sprintf("%.1f %%", percent)
	}

	if percent == 0 && p.sess.CreatedAt.IsZero() {
		return p.tr.T("perf.unknown")
	}

	return fmt.Sprintf("%.1f %% %s", percent, p.tr.T("perf.average"))
}

// cpuPercent is cpuText's numeric half, split out because the charts need the
// number and the line needs the wording. instant reports whether it is the real
// rate (a delta between two samples) or the fallback average since launch.
func (p *perfSampler) cpuPercent(sample session.ProcStats) (percent float64, instant bool) {
	if before, ok := p.previous[sample.PID]; ok {
		elapsed := sample.SampledAt.Sub(before.SampledAt)
		if elapsed > 0 {
			used := sample.CPUTime - before.CPUTime
			// A process that was replaced under the same pid, or a clock that
			// went backwards, would give a negative delta — fall through to
			// the average rather than plot a nonsense figure.
			if used >= 0 {
				return 100 * float64(used) / float64(elapsed), true
			}
		}
	}

	since := time.Since(p.sess.CreatedAt)
	if since <= 0 {
		return 0, false
	}

	return 100 * float64(sample.CPUTime) / float64(since), false
}

// formatBytes renders a byte count in binary units, which is what every tool a
// terminal user compares this against (`ps`, `htop`, `du -h`) reports.
//
// The unit names are left untranslated on purpose: "KiB"/"MiB" are IEC symbols,
// not English words, and a French "Kio" next to an `htop` showing "K" would be
// harder to compare, not easier.
func formatBytes(n uint64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := uint64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
