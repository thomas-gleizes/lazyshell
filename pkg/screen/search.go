package screen

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// cellText concatenates a line's cell contents into plain text, and records
// for each cell the byte offset in that text where its content starts —
// offsets[len(line)] is len(text). A grapheme cluster can be more than one
// byte, so a substring match found in text cannot be turned back into a
// cell range without this.
func cellText(line uv.Line) (text string, offsets []int) {
	offsets = make([]int, len(line)+1)

	var b strings.Builder
	for i, c := range line {
		offsets[i] = b.Len()
		b.WriteString(c.Content)
	}
	offsets[len(line)] = b.Len()

	return b.String(), offsets
}

// Find returns the absolute line indices whose text contains pattern (a
// case-insensitive substring match): scrollback lines first, oldest to
// newest, then the still-live rows. Absolute index i is the line RenderAt
// puts at the top of its window when called with offset =
// ScrollbackLen() - i — that is the contract callers use to scroll to a
// result. Returns nil for an empty pattern or no match.
func (s *Screen) Find(pattern string) []int {
	if pattern == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	lower := strings.ToLower(pattern)

	scrollback := s.term.Scrollback()
	scrollbackLen := scrollback.Len()

	var matches []int

	for i := range scrollbackLen {
		text, _ := cellText(scrollback.Line(i))
		if strings.Contains(strings.ToLower(text), lower) {
			matches = append(matches, i)
		}
	}

	rows := s.term.Height()
	cols := s.term.Width()

	for y := range rows {
		text, _ := cellText(s.liveLine(y, cols))
		if strings.Contains(strings.ToLower(text), lower) {
			matches = append(matches, scrollbackLen+y)
		}
	}

	return matches
}

// highlightLine returns line with AttrReverse set on every cell that is part
// of a case-insensitive occurrence of lowerPattern — an inversion rather
// than a fixed colour, so a cell's existing fg/bg still shows through, just
// swapped, the way a search highlight conventionally looks.
//
// line is never mutated in place: Scrollback.Line returns the buffer's own
// stored slice, not a copy, so writing into it here would permanently
// corrupt scrollback history the next time this same line scrolls back into
// view unhighlighted. A line with no match is returned unchanged (no clone,
// nothing to mark).
func highlightLine(line uv.Line, lowerPattern string) uv.Line {
	text, offsets := cellText(line)
	lowerText := strings.ToLower(text)

	if !strings.Contains(lowerText, lowerPattern) {
		return line
	}

	out := append(uv.Line(nil), line...)

	patternLen := len(lowerPattern)
	for start := 0; ; {
		idx := strings.Index(lowerText[start:], lowerPattern)
		if idx < 0 {
			break
		}

		matchStart := start + idx
		matchEnd := matchStart + patternLen

		for i := range out {
			if offsets[i] < matchEnd && offsets[i+1] > matchStart {
				out[i].Style.Attrs |= uv.AttrReverse
			}
		}

		start = matchEnd
	}

	return out
}
