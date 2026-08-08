package gui

import (
	"fmt"
	"strings"

	"github.com/thomas-gleizes/lazyshell/pkg/i18n"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// perfRenderer turns the resources tab's shared state into the panel's text.
//
// It samples nothing: samplePerf owns that, on its own background tick, so that
// a session's history exists whether or not anyone has been looking at it.
// This side only reads, on the render task's goroutine, and its whole job is to
// not do that work 33 times a second — hence the version check in frame.
//
// It lives in the render task's closure, and is rebuilt with it. Nothing it
// holds needs to survive that; everything that does lives in perfHistory.
type perfRenderer struct {
	sessionID string

	// gui is only used to take a snapshot, which locks gui.mu for the copy and
	// nothing else. Rendering itself touches no Gui state.
	gui *Gui

	// tr is read-only after construction and T is a map lookup, so calling it
	// from the task's goroutine is safe — unlike anything on Gui.
	tr *i18n.Catalog

	// width is the panel's inner width, captured at task start. The charts are
	// drawn to fit it, so layout restarts this task when it changes (there is
	// no other way for a task to learn it has been resized).
	width int

	// snap is what the last draw was made from, and drawn whether there has
	// been one at all — a zero version is a real version, so it cannot double
	// as "nothing drawn yet".
	snap    perfSnapshot
	drawn   bool
	content string
}

// frame returns what the resources tab should show right now, rebuilding its
// text only when the background sampler has produced something new.
func (p *perfRenderer) frame() outputFrame {
	snap, ok := p.gui.perfSnapshotFor(p.sessionID)
	if !ok {
		// Nothing sampled yet: the first background tick has not landed, or
		// this session has no process to sample.
		return outputFrame{content: "  " + p.tr.T("perf.waiting")}
	}

	if p.drawn && snap.version == p.snap.version {
		return outputFrame{content: p.content}
	}

	p.snap, p.drawn = snap, true
	p.content = p.render()

	return outputFrame{content: p.content}
}

// render builds the whole tab from the current snapshot.
func (p *perfRenderer) render() string {
	if len(p.snap.latest) == 0 {
		return "  " + p.tr.T("perf.unavailable", p.snap.err)
	}

	var b strings.Builder

	// The big chart goes first, above the per-process blocks: it is the one
	// thing on this tab worth looking at rather than reading, so it must not be
	// what falls off the bottom of a short panel.
	if chart := p.renderCPUChart(); chart != "" {
		b.WriteString(chart)
	}

	for i, sample := range p.snap.latest {
		if i > 0 {
			b.WriteString("\n")
		}

		b.WriteString(p.renderProcess(sample))
	}

	return b.String()
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
func (p *perfRenderer) renderCPUChart() string {
	if p.width < perfChartMinWidth {
		return ""
	}

	series := p.snap.cpu(p.snap.chartIsForeground)
	if len(series) < 2 {
		// A single point is not a curve; showing one would be a flat line that
		// says nothing about a series that has not started yet.
		return ""
	}

	max := maxOf(series)
	if max <= 0 {
		return ""
	}

	// Two columns of margin on each side, matching the text blocks' indent.
	width := p.width - 4

	name := p.tr.T("perf.shell")
	if p.snap.chartIsForeground {
		name = p.tr.T("perf.foreground")
	}

	lines := []string{"  " + p.tr.T("perf.cpu_chart", name, max)}
	for _, row := range brailleChart(series, max, width, cpuChartHeight) {
		lines = append(lines, "  "+row)
	}

	return strings.Join(lines, "\n") + "\n\n"
}

// renderProcess is one process's block: a heading, then one metric per line.
func (p *perfRenderer) renderProcess(sample session.ProcStats) string {
	role := p.tr.T("perf.shell")
	if sample.Foreground {
		role = p.tr.T("perf.foreground")
	}

	lines := []string{
		fmt.Sprintf("  %s — %s (pid %d)", role, sample.Comm, sample.PID),
		// CPU is scaled from zero (idle is genuinely the floor), memory on its
		// own range (a process hovering around 8.9 MiB has no meaningful zero
		// to be compared against) — see sparklineWindow.
		p.metricLine(p.tr.T("perf.cpu"), p.cpuText(sample), p.snap.cpu(sample.Foreground), true),
		p.metricLine(p.tr.T("perf.rss"), formatBytes(sample.RSSBytes), p.snap.rss(sample.Foreground), false),
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
func (p *perfRenderer) metricLine(label, value string, series []float64, fromZero bool) string {
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

// cpuText is the figure printed beside the CPU curve.
//
// It reads the last point of the series rather than recomputing anything: the
// sampler already worked the percentage out, and taking it from anywhere else
// would risk the number and the curve beside it disagreeing.
//
// A percentage is a rate, so it needs two samples. A series with only one point
// is showing cpuPercent's fallback — the average since launch — which is a real
// number but a different one, so it is labelled rather than left to pass for the
// instantaneous figure. (push resets a series when its pid changes, so "one
// point" and "first sample of this process" are the same thing.)
func (p *perfRenderer) cpuText(sample session.ProcStats) string {
	series := p.snap.cpu(sample.Foreground)
	if len(series) == 0 {
		return p.tr.T("perf.unknown")
	}

	percent := series[len(series)-1]

	if len(series) >= 2 {
		return fmt.Sprintf("%.1f %%", percent)
	}

	return fmt.Sprintf("%.1f %% %s", percent, p.tr.T("perf.average"))
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
