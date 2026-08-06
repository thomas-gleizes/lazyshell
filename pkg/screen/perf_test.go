//go:build !race

package screen

import (
	"fmt"
	"testing"
)

// fillForTest mirrors bench_test.go's fill, but against *testing.T rather
// than *testing.B — the budgets below are plain tests, not benchmarks, so
// they cannot share fill's signature.
func fillForTest(t *testing.T, s *Screen, lines int) {
	t.Helper()

	for i := range lines {
		if _, err := fmt.Fprintf(s, "\x1b[38;5;%dm%04d\x1b[0m  une ligne de sortie de commande\r\n", i%256, i); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
}

// Regression budgets for the render costs benchmarked in bench_test.go,
// measured once as plain Go tests so a CI regression fails the build instead
// of only showing up in a `go test -bench` someone has to read by hand.
// Thresholds carry a large margin (~30-50x an Apple M3 baseline) over the
// measured local ns/op so they never flake on a slower or noisier CI runner —
// they exist to catch an algorithmic regression (an O(n) turning O(n²)), not
// to track day-to-day timing drift.
//
// Excluded from -race (see the build tag above): the race detector inflates
// timings unpredictably, which would make any fixed threshold meaningless.

func TestPerfBudgetRender(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cols, rows int
		budgetNsOp int64
	}{
		{name: "80x24", cols: 80, rows: 24, budgetNsOp: 2_000_000},
		{name: "200x50", cols: 200, rows: 50, budgetNsOp: 6_000_000},
		{name: "300x80", cols: 300, rows: 80, budgetNsOp: 15_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(tc.cols, tc.rows)
			fillForTest(t, s, 20000)

			result := testing.Benchmark(func(b *testing.B) {
				for range b.N {
					_ = s.Render()
				}
			})

			if got := result.NsPerOp(); got > tc.budgetNsOp {
				t.Errorf("Render(%s) = %d ns/op, want <= %d ns/op", tc.name, got, tc.budgetNsOp)
			}
		})
	}
}

func TestPerfBudgetRenderAt(t *testing.T) {
	const budgetNsOp = 2_000_000

	s := New(80, 24)
	fillForTest(t, s, 20000)

	offset := s.ScrollbackLen() / 2

	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			_ = s.RenderAt(offset, "")
		}
	})

	if got := result.NsPerOp(); got > budgetNsOp {
		t.Errorf("RenderAt = %d ns/op, want <= %d ns/op", got, budgetNsOp)
	}
}
