package screen

import (
	"fmt"
	"strings"
	"testing"
)

// RenderAtSelection must mark every cell of a selected line in reverse video
// — the same SGR check search_test.go's TestRenderAtHighlightsMatches uses —
// and must leave a line outside the range untouched.
func TestRenderAtSelectionHighlightsTheRangeOnly(t *testing.T) {
	s := New(40, 5)
	write(t, s, "still-visible\r\n")

	// The only line on a 5-row screen with one line of output is the live
	// one, at absolute index ScrollbackLen() (Find's contract).
	abs := s.ScrollbackLen()

	plain := s.RenderAtSelection(0, -1, -1)
	selected := s.RenderAtSelection(0, abs, abs)

	if selected == plain {
		t.Error("RenderAtSelection with a range produced the same output as without one")
	}

	if !strings.Contains(selected, "\x1b[7") && !strings.Contains(selected, ";7") {
		t.Errorf("selected render has no reverse-video SGR code:\n%q", selected)
	}
}

// The same mutation guard search.go's highlightLine has: Scrollback.Line
// returns the buffer's own stored slice, so selecting a scrolled-off line
// must never leave it permanently reversed.
func TestRenderAtSelectionDoesNotMutateStoredScrollback(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		write(t, s, fmt.Sprintf("line-%d\r\n", i))
	}

	offset := s.ScrollbackLen()
	abs := 0

	selected := s.RenderAtSelection(offset, abs, abs)
	if !strings.Contains(selected, "\x1b[7") && !strings.Contains(selected, ";7") {
		t.Fatalf("expected line at abs %d to be selected at offset %d:\n%q", abs, offset, selected)
	}

	again := s.RenderAtSelection(offset, -1, -1)
	if strings.Contains(again, "\x1b[7") || strings.Contains(again, ";7") {
		t.Errorf("a later unselected RenderAtSelection of the same line still carries reverse video — stored scrollback was mutated:\n%q", again)
	}
}

// RenderAtSelection must accept the range in either order: copy-mode's
// anchor/cursor pair is not sorted by the caller.
func TestRenderAtSelectionAcceptsReversedRange(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		write(t, s, fmt.Sprintf("line-%d\r\n", i))
	}

	forward := s.RenderAtSelection(0, 2, 5)
	backward := s.RenderAtSelection(0, 5, 2)

	if forward != backward {
		t.Errorf("RenderAtSelection is not symmetric in its range: forward %q, backward %q", forward, backward)
	}
}

func TestTextRangeReturnsPlainTextAcrossScrollbackAndLive(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		write(t, s, fmt.Sprintf("line-%d\r\n", i))
	}

	// line-2 has scrolled off; line-19 (the last write) is still live.
	got := s.TextRange(0, s.ScrollbackLen()+4)

	for _, want := range []string{"line-2", "line-19"} {
		if !strings.Contains(got, want) {
			t.Errorf("TextRange(0, ...) = %q, missing %q", got, want)
		}
	}

	if strings.Contains(got, "\x1b") {
		t.Errorf("TextRange() = %q, want no escape sequences", got)
	}
}

func TestTextRangeSingleLine(t *testing.T) {
	s := New(40, 5)
	write(t, s, "exact-line\r\n")

	abs := s.ScrollbackLen()

	if got := s.TextRange(abs, abs); got != "exact-line" {
		t.Errorf("TextRange(%d, %d) = %q, want %q", abs, abs, got, "exact-line")
	}
}

func TestTextRangeAcceptsReversedRange(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		write(t, s, fmt.Sprintf("line-%d\r\n", i))
	}

	forward := s.TextRange(2, 5)
	backward := s.TextRange(5, 2)

	if forward != backward {
		t.Errorf("TextRange is not symmetric in its range: forward %q, backward %q", forward, backward)
	}
}

// A stale selection (a resize, or more output arriving and pushing
// ScrollbackLen() up) must clamp rather than panic or return garbage.
func TestTextRangeClampsOutOfBoundsIndices(t *testing.T) {
	s := New(40, 5)
	write(t, s, "only-line\r\n")

	if got := s.TextRange(-100, 100000); !strings.Contains(got, "only-line") {
		t.Errorf("TextRange with wildly out-of-range indices = %q, want it to contain %q", got, "only-line")
	}
}
