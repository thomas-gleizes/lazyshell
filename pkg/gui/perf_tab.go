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

	content string
	takenAt time.Time
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

	var b strings.Builder

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

// renderProcess is one process's block: a heading, then one metric per line.
func (p *perfSampler) renderProcess(sample session.ProcStats) string {
	role := p.tr.T("perf.shell")
	if sample.Foreground {
		role = p.tr.T("perf.foreground")
	}

	lines := []string{
		fmt.Sprintf("  %s — %s (pid %d)", role, sample.Comm, sample.PID),
		fmt.Sprintf("    %-10s %s", p.tr.T("perf.cpu"), p.cpuText(sample)),
		fmt.Sprintf("    %-10s %s", p.tr.T("perf.rss"), formatBytes(sample.RSSBytes)),
	}

	threads := p.tr.T("perf.unknown")
	if sample.ThreadsAvailable {
		threads = fmt.Sprintf("%d", sample.Threads)
	}

	lines = append(lines, fmt.Sprintf("    %-10s %s", p.tr.T("perf.threads"), threads))

	disk := p.tr.T("perf.unavailable_os")
	if sample.DiskIOAvailable {
		disk = p.tr.T("perf.disk_io", formatBytes(sample.DiskRead), formatBytes(sample.DiskWritten))
	}

	lines = append(lines, fmt.Sprintf("    %-10s %s", p.tr.T("perf.disk"), disk), "")

	return strings.Join(lines, "\n")
}

// cpuText turns a cumulative CPU time into a percentage.
//
// A percentage is a rate, so it needs two samples. The first one after the tab
// is opened has none to compare against, and rather than show a blank it falls
// back to the process's average since it started — a real number, but a
// different one, so it is labelled as such instead of quietly passing for the
// instantaneous figure.
func (p *perfSampler) cpuText(sample session.ProcStats) string {
	if before, ok := p.previous[sample.PID]; ok {
		elapsed := sample.SampledAt.Sub(before.SampledAt)
		if elapsed > 0 {
			used := sample.CPUTime - before.CPUTime
			// A process that was replaced under the same pid, or a clock that
			// went backwards, would give a negative delta — show nothing
			// rather than a nonsense figure.
			if used >= 0 {
				return fmt.Sprintf("%.1f %%", 100*float64(used)/float64(elapsed))
			}
		}
	}

	since := time.Since(p.sess.CreatedAt)
	if since <= 0 {
		return p.tr.T("perf.unknown")
	}

	return fmt.Sprintf("%.1f %% %s", 100*float64(sample.CPUTime)/float64(since), p.tr.T("perf.average"))
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
