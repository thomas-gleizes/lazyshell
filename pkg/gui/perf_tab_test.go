package gui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/i18n"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		in   uint64
		want string
	}{
		{name: "zero", in: 0, want: "0 B"},
		{name: "below a kibibyte", in: 512, want: "512 B"},
		{name: "exactly a kibibyte", in: 1024, want: "1.0 KiB"},
		{name: "mebibytes", in: 8*1024*1024 + 512*1024, want: "8.5 MiB"},
		{name: "gibibytes", in: 3 * 1024 * 1024 * 1024, want: "3.0 GiB"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBytes(tc.in); got != tc.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A percentage is a rate: with two samples it is the delta, and without one it
// has to fall back to something and say so.
func TestCPUPercent(t *testing.T) {
	now := time.Now()
	created := now.Add(-10 * time.Second)

	t.Run("delta between two samples", func(t *testing.T) {
		before := session.ProcStats{PID: 42, CPUTime: time.Second, SampledAt: now}
		after := session.ProcStats{PID: 42, CPUTime: 1500 * time.Millisecond, SampledAt: now.Add(time.Second)}

		// Half a second of CPU over one second of wall clock.
		got, instant := cpuPercent(before, after, created)

		if !instant {
			t.Error("instant = false with two samples to compare")
		}

		if got != 50 {
			t.Errorf("cpuPercent = %v, want 50", got)
		}
	})

	t.Run("no previous sample falls back to the average", func(t *testing.T) {
		// One second of CPU over the ten the session has been alive. cpuPercent
		// computes time.Since(created) at call time, so the expected value is
		// derived from the same clock rather than assuming a fixed window —
		// a hardcoded band flaked on loaded/-race CI runners where more than a
		// beat of real time passes between capturing `created` and this call.
		before := time.Since(created)
		got, instant := cpuPercent(session.ProcStats{}, session.ProcStats{
			PID: 42, CPUTime: time.Second, SampledAt: now,
		}, created)
		after := time.Since(created)

		if instant {
			t.Error("instant = true with nothing to compare against")
		}

		wantMax := 100 * float64(time.Second) / float64(before)
		wantMin := 100 * float64(time.Second) / float64(after)
		if got < wantMin || got > wantMax {
			t.Errorf("cpuPercent = %v, want the ~10%% average (between %v and %v)", got, wantMin, wantMax)
		}
	})

	t.Run("a pid reused under us falls back too", func(t *testing.T) {
		// A negative delta means the pid is not the process it was.
		before := session.ProcStats{PID: 42, CPUTime: 10 * time.Second, SampledAt: now}
		after := session.ProcStats{PID: 42, CPUTime: time.Second, SampledAt: now.Add(time.Second)}

		if _, instant := cpuPercent(before, after, created); instant {
			t.Error("instant = true on a negative delta, want the average fallback")
		}
	})
}

// newTestRenderer is a perfRenderer over a fixed snapshot, for the rendering
// tests — none of which want a real session or a sampler behind them.
func newTestRenderer(width int, snap perfSnapshot) *perfRenderer {
	return &perfRenderer{tr: i18n.New("fr"), width: width, snap: snap, drawn: true}
}

// The figure and the curve beside it must come from the same place, or they can
// disagree — hence cpuText reading the series rather than recomputing.
func TestCPUTextReadsTheSeries(t *testing.T) {
	tests := []struct {
		name   string
		series []float64
		want   string
		avg    bool
	}{
		{name: "no series at all", series: nil, want: "inconnu"},
		{name: "a single point is the average fallback", series: []float64{12.5}, want: "12.5 %", avg: true},
		{name: "two points is a real rate", series: []float64{12.5, 40}, want: "40.0 %"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestRenderer(100, perfSnapshot{shellCPU: tc.series})

			got := p.cpuText(session.ProcStats{PID: 1})

			if !strings.Contains(got, tc.want) {
				t.Errorf("cpuText = %q, want it to contain %q", got, tc.want)
			}

			// The fallback is a different number than the one it stands in for,
			// so it must never pass for the instantaneous figure.
			if labelled := strings.Contains(got, "moy."); labelled != tc.avg {
				t.Errorf("cpuText = %q, average label = %v, want %v", got, labelled, tc.avg)
			}
		})
	}
}

// What a platform cannot report must read as "unknown", never as zero: a
// process showing "0 threads" and "0 B written" would be a lie.
func TestRenderProcessMarksUnavailableMetrics(t *testing.T) {
	p := newTestRenderer(100, perfSnapshot{})

	got := p.renderProcess(session.ProcStats{
		PID:              42,
		Comm:             "sh",
		RSSBytes:         1024,
		ThreadsAvailable: false,
		DiskIOAvailable:  false,
	})

	if strings.Contains(got, "threads    0") {
		t.Errorf("an unavailable thread count is rendered as zero:\n%s", got)
	}

	if !strings.Contains(got, "inconnu") {
		t.Errorf("no unknown marker for the thread count:\n%s", got)
	}

	if !strings.Contains(got, "indisponible") {
		t.Errorf("no unavailable marker for the disk I/O:\n%s", got)
	}
}

func TestRenderProcessNamesBothRoles(t *testing.T) {
	p := newTestRenderer(100, perfSnapshot{})

	shell := p.renderProcess(session.ProcStats{PID: 1, Comm: "zsh"})
	fg := p.renderProcess(session.ProcStats{PID: 2, Comm: "claude", Foreground: true})

	if !strings.Contains(shell, "shell") || !strings.Contains(shell, "zsh") {
		t.Errorf("the shell's block does not name it:\n%s", shell)
	}

	if !strings.Contains(fg, "avant-plan") || !strings.Contains(fg, "claude") {
		t.Errorf("the foreground block does not name it:\n%s", fg)
	}
}

// The whole reason the renderer holds a version: rebuilding this text on every
// 30 ms redraw would undo the point of sampling slowly.
func TestRendererReusesItsTextUntilANewSample(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	history := &perfHistory{
		version: 1,
		latest:  []session.ProcStats{{PID: 1, Comm: "sh", RSSBytes: 1024}},
	}
	gui.perfHistories = map[string]*perfHistory{"s1": history}

	p := &perfRenderer{sessionID: "s1", gui: gui, tr: i18n.New("fr"), width: 100}

	first := p.frame()
	if first.content == "" {
		t.Fatal("the first frame is empty")
	}

	// Same version: the text must be handed back untouched, not rebuilt.
	p.content = "SENTINEL"
	if got := p.frame(); got.content != "SENTINEL" {
		t.Error("the renderer rebuilt its text without a new sample")
	}

	gui.mu.Lock()
	history.version++
	history.latest = []session.ProcStats{{PID: 1, Comm: "sh", RSSBytes: 2048}}
	gui.mu.Unlock()

	if got := p.frame(); got.content == "SENTINEL" {
		t.Error("the renderer did not redraw after a new sample")
	}
}

// Before the first background tick lands there is nothing to draw, and the
// panel has to say that rather than go blank.
func TestRendererSaysWhenNothingIsSampledYet(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	p := &perfRenderer{sessionID: "absent", gui: gui, tr: i18n.New("fr"), width: 100}

	got := p.frame()

	if !strings.Contains(got.content, "Mesure en cours") {
		t.Errorf("content = %q, want it to say sampling is under way", got.content)
	}

	// And no cursor: this tab is not a screen being typed into.
	if got.cursorShown {
		t.Error("the resources tab asked for a terminal cursor")
	}
}

// A session whose processes could not be read at all must say why.
func TestRendererReportsAnUnmeasurableSession(t *testing.T) {
	p := newTestRenderer(100, perfSnapshot{err: errors.New("session-1")})

	if got := p.render(); !strings.Contains(got, "Mesure impossible") {
		t.Errorf("render = %q, want it to report why there are no figures", got)
	}
}

// The charts only appear once there is a series to draw; before that the tab is
// the figures alone rather than an empty frame.
func TestChartsAppearOnlyOnceThereIsHistory(t *testing.T) {
	tests := []struct {
		name   string
		series []float64
		want   bool
	}{
		{name: "nothing sampled", series: nil},
		{name: "a single point is not a curve", series: []float64{10}},
		{name: "all idle has no scale to draw against", series: []float64{0, 0}},
		{name: "two points and some load", series: []float64{10, 40}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestRenderer(100, perfSnapshot{shellCPU: tc.series})

			chart := p.renderCPUChart()

			if (chart != "") != tc.want {
				t.Fatalf("renderCPUChart drew %q, want drawn = %v", chart, tc.want)
			}

			// The peak is what the auto-scaled vertical axis is anchored on, so
			// it has to be stated — a curve on an unlabelled moving scale means
			// nothing.
			if tc.want && !strings.Contains(chart, "40.0") {
				t.Errorf("the chart does not state its peak:\n%s", chart)
			}
		})
	}
}

// A panel too narrow for a chart must fall back to the figures rather than draw
// a stub of one.
func TestChartsAreDroppedOnANarrowPanel(t *testing.T) {
	snap := perfSnapshot{shellCPU: []float64{10, 40}}

	p := newTestRenderer(perfChartMinWidth-1, snap)

	if got := p.renderCPUChart(); got != "" {
		t.Errorf("a chart was drawn on a %d-column panel:\n%s", p.width, got)
	}

	line := p.metricLine("CPU", "40.0 %", snap.shellCPU, true)
	if strings.ContainsAny(line, string(sparkLevels)) {
		t.Errorf("a sparkline was drawn on a %d-column panel: %q", p.width, line)
	}
}
