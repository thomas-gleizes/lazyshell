package gui

import (
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
// has to say that it is showing something else instead.
func TestPerfSamplerCPUText(t *testing.T) {
	now := time.Now()

	sampler := func(previous map[int]session.ProcStats) *perfSampler {
		return &perfSampler{
			tr:       i18n.New("fr"),
			previous: previous,
			sess:     &session.Session{CreatedAt: now.Add(-10 * time.Second)},
		}
	}

	t.Run("delta between two samples", func(t *testing.T) {
		// Half a second of CPU over one second of wall clock is 50%.
		p := sampler(map[int]session.ProcStats{
			42: {PID: 42, CPUTime: time.Second, SampledAt: now},
		})

		got := p.cpuText(session.ProcStats{
			PID:       42,
			CPUTime:   1500 * time.Millisecond,
			SampledAt: now.Add(time.Second),
		})

		if got != "50.0 %" {
			t.Errorf("cpuText = %q, want %q", got, "50.0 %")
		}
	})

	t.Run("first sample falls back to the average", func(t *testing.T) {
		p := sampler(nil)

		// One second of CPU over the ten the session has been alive.
		got := p.cpuText(session.ProcStats{PID: 42, CPUTime: time.Second, SampledAt: now})

		if !strings.HasPrefix(got, "10.0 %") {
			t.Errorf("cpuText = %q, want it to start with %q", got, "10.0 %")
		}

		// And it must say so rather than pass for the instantaneous figure.
		if !strings.Contains(got, "moy.") {
			t.Errorf("cpuText = %q, want it labelled as an average", got)
		}
	})

	t.Run("a pid reused under us shows no figure", func(t *testing.T) {
		// A negative delta means the pid is not the process it was.
		p := sampler(map[int]session.ProcStats{
			42: {PID: 42, CPUTime: 10 * time.Second, SampledAt: now},
		})

		got := p.cpuText(session.ProcStats{
			PID:       42,
			CPUTime:   time.Second,
			SampledAt: now.Add(time.Second),
		})

		if !strings.Contains(got, "moy.") {
			t.Errorf("cpuText = %q, want it to fall back to the average", got)
		}
	})
}

// What a platform cannot report must read as "unknown", never as zero: a
// process showing "0 threads" and "0 B written" would be a lie.
func TestPerfSamplerMarksUnavailableMetrics(t *testing.T) {
	p := &perfSampler{tr: i18n.New("fr"), sess: &session.Session{CreatedAt: time.Now()}}

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

func TestPerfSamplerRendersBothProcesses(t *testing.T) {
	p := &perfSampler{tr: i18n.New("fr"), sess: &session.Session{CreatedAt: time.Now()}}

	shell := p.renderProcess(session.ProcStats{PID: 1, Comm: "zsh"})
	fg := p.renderProcess(session.ProcStats{PID: 2, Comm: "claude", Foreground: true})

	if !strings.Contains(shell, "shell") || !strings.Contains(shell, "zsh") {
		t.Errorf("the shell's block does not name it:\n%s", shell)
	}

	if !strings.Contains(fg, "avant-plan") || !strings.Contains(fg, "claude") {
		t.Errorf("the foreground block does not name it:\n%s", fg)
	}
}

// The sampler exists to decouple sampling from the 30 ms redraw tick; a
// regression here would spawn a `ps` 33 times a second on macOS.
func TestPerfSamplerHonoursItsInterval(t *testing.T) {
	m := newTestSessionManager(t)

	sess, err := m.New("perf", "/bin/sh")
	if err != nil {
		t.Fatalf("Manager.New: %v", err)
	}

	p := &perfSampler{sess: sess, tr: i18n.New("fr"), interval: time.Hour}

	p.frame()
	first := p.takenAt

	if first.IsZero() {
		t.Fatal("the first frame did not sample")
	}

	for range 5 {
		p.frame()
	}

	if !p.takenAt.Equal(first) {
		t.Error("the sampler re-sampled inside its interval")
	}
}

// And the converse: once the interval has elapsed, it must actually re-sample.
func TestPerfSamplerResamplesAfterItsInterval(t *testing.T) {
	m := newTestSessionManager(t)

	sess, err := m.New("perf", "/bin/sh")
	if err != nil {
		t.Fatalf("Manager.New: %v", err)
	}

	p := &perfSampler{sess: sess, tr: i18n.New("fr"), interval: time.Millisecond}

	p.frame()
	first := p.takenAt

	time.Sleep(5 * time.Millisecond)
	p.frame()

	if p.takenAt.Equal(first) {
		t.Error("the sampler did not re-sample after its interval elapsed")
	}
}

// An exited session cannot be sampled — the panel has to say so rather than go
// blank or keep showing figures that are no longer about anything.
func TestPerfSamplerReportsAnUnmeasurableSession(t *testing.T) {
	p := &perfSampler{
		sess:     &session.Session{ID: "gone", CreatedAt: time.Now()},
		tr:       i18n.New("fr"),
		interval: time.Hour,
	}

	got := p.frame()

	if !strings.Contains(got.content, "Mesure impossible") {
		t.Errorf("content = %q, want it to report why there are no figures", got.content)
	}

	// And no cursor: perf is not a screen being typed into.
	if got.cursorShown {
		t.Error("the perf tab asked for a terminal cursor")
	}
}

// newTestSessionManager is a Manager whose sessions are killed and drained
// before the test returns, so none outlives it.
func newTestSessionManager(t *testing.T) *session.Manager {
	t.Helper()

	m := session.NewManager()
	m.KillTimeout = 300 * time.Millisecond

	t.Cleanup(m.Shutdown)

	return m
}
