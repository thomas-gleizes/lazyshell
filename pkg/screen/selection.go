package screen

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// RenderAtSelection is RenderAt's copy-mode sibling: the same offset-lines-
// back window, but with every full line whose absolute index (Find's
// contract) falls in [fromAbs, toAbs] marked in reverse video, instead of
// highlighting a substring match. fromAbs/toAbs may be given in either order.
//
// Copy-mode selects whole lines, never a column range, so this needs none of
// highlightLine's per-cell text matching — selectLine below just reverses
// every cell of a line that is in range.
func (s *Screen) RenderAtSelection(offset, fromAbs, toAbs int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fromAbs > toAbs {
		fromAbs, toAbs = toAbs, fromAbs
	}

	start, _ := s.windowStart(offset)
	lines := s.linesAt(offset)

	for i, line := range lines {
		if abs := start + i; abs >= fromAbs && abs <= toAbs {
			lines[i] = selectLine(line)
		}
	}

	return lines.Render()
}

// selectLine returns line with AttrReverse set on every cell. Never mutates
// line in place, same reasoning as highlightLine in search.go: Scrollback.Line
// returns the buffer's own stored slice, and writing into it would corrupt
// history the next time this line scrolls back into view.
func selectLine(line uv.Line) uv.Line {
	out := append(uv.Line(nil), line...)

	for i := range out {
		out[i].Style.Attrs |= uv.AttrReverse
	}

	return out
}

// TextRange returns the plain text (no SGR) of every line whose absolute
// index (Find's contract) falls in [fromAbs, toAbs], oldest first, joined by
// "\n" — copy-mode's yank and the scrollback export both build on this.
// fromAbs/toAbs may be given in either order; both are clamped to the
// addressable range, so a selection made against a screen that has since
// grown or shrunk (a resize, more output arriving) never panics.
func (s *Screen) TextRange(fromAbs, toAbs int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fromAbs > toAbs {
		fromAbs, toAbs = toAbs, fromAbs
	}

	if fromAbs < 0 {
		fromAbs = 0
	}

	scrollback := s.term.Scrollback()
	scrollbackLen := scrollback.Len()
	rows := s.term.Height()
	cols := s.term.Width()

	if maxIdx := scrollbackLen + rows - 1; toAbs > maxIdx {
		toAbs = maxIdx
	}

	var b strings.Builder

	for i := fromAbs; i <= toAbs; i++ {
		var line uv.Line
		if i < scrollbackLen {
			line = scrollback.Line(i)
		} else {
			line = s.liveLine(i-scrollbackLen, cols)
		}

		text, _ := cellText(line)

		if i > fromAbs {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(text, " "))
	}

	return b.String()
}
