package gui

import (
	"strings"
	"testing"
)

func TestSparkline(t *testing.T) {
	tests := []struct {
		name    string
		values  []float64
		lo, hi  float64
		width   int
		want    string
		comment string
	}{
		{
			name:   "levels span the block characters",
			values: []float64{0, 100},
			lo:     0,
			hi:     100,
			width:  2,
			want:   "▁█",
		},
		{
			// A short history must not be stretched to fill the width, or the
			// curve would change shape on every new point.
			name:   "right aligned, blanks on the left",
			values: []float64{100},
			lo:     0,
			hi:     100,
			width:  4,
			want:   "   █",
		},
		{
			name:   "longer than the width keeps the newest",
			values: []float64{0, 0, 0, 100},
			lo:     0,
			hi:     100,
			width:  2,
			want:   "▁█",
		},
		{
			// Guarded rather than drawing a misleading flat line at full height.
			name:   "degenerate window draws nothing",
			values: []float64{5, 5},
			lo:     5,
			hi:     5,
			width:  2,
			want:   "  ",
		},
		{
			name:   "no width, no output",
			values: []float64{1},
			lo:     0,
			hi:     1,
			width:  0,
			want:   "",
		},
		{
			// The window is not always anchored at zero — see sparklineWindow.
			name:   "offset window",
			values: []float64{100, 108},
			lo:     100,
			hi:     108,
			width:  2,
			want:   "▁█",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sparkline(tc.values, tc.lo, tc.hi, tc.width); got != tc.want {
				t.Errorf("sparkline = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSparklineWindow(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		fromZero bool
		lo, hi   float64
	}{
		{
			name:     "from zero uses the observed peak",
			values:   []float64{0, 12, 4},
			fromZero: true,
			lo:       0,
			hi:       12,
		},
		{
			// An idle process must read as sitting on the floor, not as a
			// degenerate window that draws blanks.
			name:     "from zero with nothing observed still has a span",
			values:   []float64{0, 0},
			fromZero: true,
			lo:       0,
			hi:       1,
		},
		{
			// Memory: scaled from zero, a process hovering around 8.9 MiB is a
			// flat line at the ceiling that says nothing.
			name:   "range scaling frames the series on itself",
			values: []float64{100, 104, 108},
			lo:     100,
			hi:     108,
		},
		{
			// Centred, so a never-moving series draws flat at mid height rather
			// than reading as "zero" or as "maxed out".
			name:   "a flat series gets a window around its value",
			values: []float64{42, 42, 42},
			lo:     41,
			hi:     43,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lo, hi := sparklineWindow(tc.values, tc.fromZero)

			if lo != tc.lo || hi != tc.hi {
				t.Errorf("sparklineWindow = (%v, %v), want (%v, %v)", lo, hi, tc.lo, tc.hi)
			}

			if hi <= lo {
				t.Error("sparklineWindow returned a degenerate window, which callers rely on never happening")
			}
		})
	}
}

// A flat series must land in the middle of the row — the whole point of the
// centred window above.
func TestSparklineDrawsAFlatSeriesAtMidHeight(t *testing.T) {
	values := []float64{42, 42, 42}

	lo, hi := sparklineWindow(values, false)

	got := sparkline(values, lo, hi, 3)
	if strings.ContainsAny(got, "▁█") {
		t.Errorf("sparkline = %q, want it away from both the floor and the ceiling", got)
	}
}

func TestBrailleChart(t *testing.T) {
	// A braille cell is two dot columns wide, so one full-height value fills
	// only the right half of a one-character-wide chart, and two fill both.
	tests := []struct {
		name          string
		values        []float64
		max           float64
		width, height int
		want          []string
	}{
		{
			name:   "one full value fills the right dot column",
			values: []float64{10},
			max:    10,
			width:  1,
			height: 1,
			want:   []string{"⢸"},
		},
		{
			name:   "two full values fill the whole cell",
			values: []float64{10, 10},
			max:    10,
			width:  1,
			height: 1,
			want:   []string{"⣿"},
		},
		{
			// Zero really is empty: a blank column and an idle column must look
			// the same, because they mean the same thing.
			name:   "zero leaves the cell empty",
			values: []float64{0, 0},
			max:    10,
			width:  1,
			height: 1,
			want:   []string{"⠀"},
		},
		{
			name:   "no width gives no chart",
			values: []float64{1},
			max:    1,
			width:  0,
			height: 2,
			want:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := brailleChart(tc.values, tc.max, tc.width, tc.height)

			if len(got) != len(tc.want) {
				t.Fatalf("brailleChart returned %d rows (%q), want %d", len(got), got, len(tc.want))
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("row %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// A process using a sliver of a CPU is doing something; an empty column would
// be indistinguishable from idle.
func TestBrailleChartKeepsATinyValueVisible(t *testing.T) {
	got := brailleChart([]float64{0.01, 0.01}, 100, 1, 4)

	joined := strings.Join(got, "")
	if strings.Count(joined, "⠀") == len(got) {
		t.Errorf("a tiny non-zero value drew nothing at all: %q", got)
	}
}

func TestBrailleChartFillsTheRequestedShape(t *testing.T) {
	const width, height = 20, 4

	got := brailleChart([]float64{1, 2, 3}, 3, width, height)

	if len(got) != height {
		t.Fatalf("got %d rows, want %d", len(got), height)
	}

	for i, row := range got {
		if n := len([]rune(row)); n != width {
			t.Errorf("row %d is %d runes wide, want %d", i, n, width)
		}
	}
}
