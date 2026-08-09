//go:build !race

package gui

import (
	"fmt"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// Regression budgets for the render costs benchmarked in bench_test.go. See
// pkg/screen/perf_test.go for why these are plain tests with a fixed
// threshold rather than committed benchmark output, and why -race is
// excluded via the build tag above.

// sessionsForTest mirrors bench_test.go's benchSessions, but against
// *testing.T rather than *testing.B.
func sessionsForTest(t *testing.T, gui *Gui, n int) []*session.Session {
	t.Helper()

	sessions := make([]*session.Session, 0, n)

	for i := range n {
		sess, err := gui.sessions.New(fmt.Sprintf("s%d", i), "/bin/sh")
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		for j := range 2000 {
			if _, err := fmt.Fprintf(sess.Screen(), "\x1b[38;5;%dm%04d\x1b[0m  sortie\r\n", j%256, j); err != nil {
				t.Fatalf("Screen().Write: %v", err)
			}
		}

		sessions = append(sessions, sess)
	}

	return sessions
}

func TestPerfBudgetBuildOutputFrame(t *testing.T) {
	const budgetNsOp = 3_000_000

	gui, _ := newHeadlessGui(t)
	sessions := sessionsForTest(t, gui, 1)

	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			_ = buildOutputFrame(sessions[0], 0, true, "", -1, -1)
		}
	})

	if got := result.NsPerOp(); got > budgetNsOp {
		t.Errorf("buildOutputFrame = %d ns/op, want <= %d ns/op", got, budgetNsOp)
	}
}

// The env tab is rendered once per task, not per tick, so its cost is paid on
// every tab switch and every selection change — cheap enough not to be felt
// there, but it sorts a few hundred strings, which is not free either.
func TestPerfBudgetEnvTabContent(t *testing.T) {
	const (
		vars       = 300
		budgetNsOp = 500_000
	)

	gui, _ := newHeadlessGui(t)

	env := make([]string, vars)
	for i := range env {
		env[i] = fmt.Sprintf("LAZYSHELL_VAR_%03d=/some/reasonably/long/value/%d", i, i)
	}

	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			_ = gui.envTabContent(env)
		}
	})

	if got := result.NsPerOp(); got > budgetNsOp {
		t.Errorf("envTabContent(%d vars) = %d ns/op, want <= %d ns/op", vars, got, budgetNsOp)
	}
}

// The charts are redrawn on every perf sample, i.e. about once a second, so
// they have a whole order of magnitude more headroom than the render tick —
// but they are also the only thing on this tab that loops over a full history,
// which is what this guards.
func TestPerfBudgetBrailleChart(t *testing.T) {
	const budgetNsOp = 200_000

	series := make([]float64, perfHistoryLen)
	for i := range series {
		series[i] = float64(i % 97)
	}

	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			_ = brailleChart(series, 96, 200, cpuChartHeight)
		}
	})

	if got := result.NsPerOp(); got > budgetNsOp {
		t.Errorf("brailleChart(%d points) = %d ns/op, want <= %d ns/op", len(series), got, budgetNsOp)
	}
}

func TestPerfBudgetSessionsPanelContent(t *testing.T) {
	const (
		n          = 16
		budgetNsOp = 500_000
	)

	gui, _ := newHeadlessGui(t)
	sessions := sessionsForTest(t, gui, n)

	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			_ = sessionsPanelContent(sessions, testMarkers, "", nil, nil, nil, 0)
		}
	})

	if got := result.NsPerOp(); got > budgetNsOp {
		t.Errorf("sessionsPanelContent(%d sessions, 0) = %d ns/op, want <= %d ns/op", n, got, budgetNsOp)
	}
}
