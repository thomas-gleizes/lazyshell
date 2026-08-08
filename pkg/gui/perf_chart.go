package gui

import "strings"

// Chart drawing for the resources tab. Two shapes, for two jobs:
//
//   - sparkline: one character row, glued to the figure it illustrates. It
//     answers "is this going up?" and nothing more, which is all a line next to
//     a number needs to answer.
//   - brailleChart: a real plot, several rows tall, for the one metric worth
//     looking at properly — the foreground process's CPU.
//
// Both are pure functions over a slice of samples, so they are cheap to test
// and carry no state: the history they read lives in pkg/gui/perf_tab.go.
//
// Both also draw *right-aligned*: the newest sample is at the right edge and a
// short history leaves the left blank, so the curve grows leftwards from the
// present instead of stretching to fill the width and changing shape on every
// new point.

// sparkLevels are the eighth-block characters, lowest to highest. Eight levels
// is all a single row can carry.
var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// sparkline renders values as a single row, width characters wide, with lo at
// the bottom of the row and hi at the top.
//
// The window is a parameter rather than always [0, max] because the two metrics
// on this tab want different ones. CPU is meaningful from zero — idle really is
// the floor. Memory is not: a process sitting between 8.8 and 8.9 MiB scaled
// from zero is a flat line at the ceiling, which is exactly what the first
// version drew and exactly what tells the user nothing. Framing memory on its
// own observed range is what makes its variation visible at all.
//
// Callers must pass hi > lo; sparklineWindow is what guarantees it.
func sparkline(values []float64, lo, hi float64, width int) string {
	if width <= 0 {
		return ""
	}

	span := hi - lo

	var b strings.Builder

	for _, value := range alignRight(values, width) {
		if value.blank || span <= 0 {
			b.WriteRune(' ')

			continue
		}

		b.WriteRune(sparkLevels[levelOf(value.v-lo, span, len(sparkLevels))])
	}

	return b.String()
}

// sparklineWindow picks the vertical window for a series.
//
// fromZero anchors the bottom at zero (CPU); otherwise the window is the
// series' own range (memory). Either way it returns hi > lo, so the caller
// never has to reason about a degenerate scale:
//
//   - an all-zero from-zero series gets [0, 1], which draws it along the floor
//     — idle, which is the truth;
//   - a series that never moves gets a window centred on its value, which draws
//     it flat at mid height rather than pinned to the floor (reads as "zero")
//     or the ceiling (reads as "maxed out").
func sparklineWindow(series []float64, fromZero bool) (lo, hi float64) {
	hi = maxOf(series)

	if fromZero {
		if hi <= 0 {
			return 0, 1
		}

		return 0, hi
	}

	lo = minOf(series)
	if hi <= lo {
		return hi - 1, hi + 1
	}

	return lo, hi
}

// Braille cells are a 2x4 dot grid, which is what makes them worth the trouble:
// four times the vertical resolution of the block characters above, for the
// same one character cell. The bit for each dot is fixed by Unicode — the
// pattern is U+2800 plus the OR of the bits of the dots that are raised.
//
//	dot layout        bit
//	 (0,0) (0,1)     0x01 0x08
//	 (1,0) (1,1)     0x02 0x10
//	 (2,0) (2,1)     0x04 0x20
//	 (3,0) (3,1)     0x40 0x80
const brailleBase = 0x2800

var brailleDotBits = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// brailleChart renders values as a filled area chart, height character rows
// tall and width characters wide.
//
// Filled rather than a bare line: at this resolution a one-dot-thick line
// through a noisy series reads as scattered specks, while a filled area still
// shows the shape. It is also what makes the chart legible at a glance next to
// a number, which is the whole point of putting it there.
func brailleChart(values []float64, max float64, width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}

	dotRows := height * 4

	// grid[row][col] accumulates the raised dots of each character cell.
	grid := make([][]rune, height)
	for row := range grid {
		grid[row] = make([]rune, width)
		for col := range grid[row] {
			grid[row][col] = brailleBase
		}
	}

	for i, value := range alignRight(values, width*2) {
		if value.blank || max <= 0 {
			continue
		}

		// A non-zero value always raises at least the bottom dot: a process
		// using 0.1% of a CPU is doing something, and a chart that showed it
		// as an empty column would be indistinguishable from one that showed
		// it as idle.
		filled := levelOf(value.v, max, dotRows+1)
		if filled == 0 && value.v > 0 {
			filled = 1
		}

		charCol := i / 2
		dotCol := i % 2

		for n := range filled {
			dotRow := dotRows - 1 - n
			grid[dotRow/4][charCol] |= brailleDotBits[dotRow%4][dotCol]
		}
	}

	lines := make([]string, height)
	for row := range grid {
		lines[row] = string(grid[row])
	}

	return lines
}

// sample is one slot of a right-aligned series: either a value, or a blank
// standing for "no history reaches back this far".
type sample struct {
	v     float64
	blank bool
}

// alignRight fits values into exactly n slots, keeping the most recent ones and
// padding the left with blanks when there are not enough.
func alignRight(values []float64, n int) []sample {
	out := make([]sample, n)

	if len(values) > n {
		values = values[len(values)-n:]
	}

	pad := n - len(values)
	for i := range out {
		if i < pad {
			out[i] = sample{blank: true}

			continue
		}

		out[i] = sample{v: values[i-pad]}
	}

	return out
}

// levelOf maps a value in [0, max] onto one of levels discrete steps.
func levelOf(value, max float64, levels int) int {
	if value <= 0 || max <= 0 {
		return 0
	}

	level := int(value / max * float64(levels))
	if level >= levels {
		return levels - 1
	}

	if level < 0 {
		return 0
	}

	return level
}
