package screen

import (
	"fmt"
	"strings"
	"testing"
)

// Find must locate a pattern that has scrolled off into history, not just
// what is still on the live screen — that is the whole point of searching a
// scrollback instead of just grepping the visible window.
func TestFindMatchesInScrollback(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		write(t, s, fmt.Sprintf("line-%d\r\n", i))
	}

	if strings.Contains(s.Render(), "line-2") {
		t.Fatal("line-2 should already have scrolled off the live screen")
	}

	matches := s.Find("line-2")
	if len(matches) == 0 {
		t.Fatal("Find did not locate a pattern that only exists in scrollback")
	}
}

// A pattern still on the live screen must be found too, at an index at or
// beyond ScrollbackLen() — the boundary jumpToMatch relies on to decide
// whether a match needs scrolling at all.
func TestFindMatchesOnLiveScreen(t *testing.T) {
	s := New(40, 5)
	write(t, s, "still-visible\r\n")

	matches := s.Find("still-visible")
	if len(matches) != 1 {
		t.Fatalf("Find() = %v, want exactly one match", matches)
	}

	if matches[0] < s.ScrollbackLen() {
		t.Errorf("live match index %d should be >= ScrollbackLen() %d", matches[0], s.ScrollbackLen())
	}
}

func TestFindIsCaseInsensitive(t *testing.T) {
	s := New(40, 5)
	write(t, s, "Hello World\r\n")

	if len(s.Find("hello world")) == 0 {
		t.Error("Find should match regardless of case")
	}
}

func TestFindEmptyOrAbsentPatternReturnsNil(t *testing.T) {
	s := New(40, 5)
	write(t, s, "something\r\n")

	if got := s.Find(""); got != nil {
		t.Errorf("Find(\"\") = %v, want nil", got)
	}

	if got := s.Find("nowhere-to-be-found"); got != nil {
		t.Errorf("Find on an absent pattern = %v, want nil", got)
	}
}

// RenderAt with a highlight must mark the matching text (AttrReverse) —
// checked via the SGR "reverse video" code (7) that uv.Style renders for it.
func TestRenderAtHighlightsMatches(t *testing.T) {
	s := New(40, 5)
	write(t, s, "find-me\r\n")

	plain := s.RenderAt(0, "")
	highlighted := s.RenderAt(0, "find-me")

	if highlighted == plain {
		t.Error("RenderAt with a highlight produced the same output as without one")
	}

	if !strings.Contains(highlighted, "\x1b[7") && !strings.Contains(highlighted, ";7") && !strings.Contains(highlighted, ";7m") {
		t.Errorf("highlighted render has no reverse-video SGR code:\n%q", highlighted)
	}
}

// The critical regression this guards against: Scrollback.Line returns the
// buffer's own stored slice, not a copy. Highlighting it in place would
// permanently corrupt scrollback history — a line highlighted once would
// stay highlighted forever, even for a later, different (or no) search.
func TestHighlightDoesNotMutateStoredScrollback(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		write(t, s, fmt.Sprintf("line-%d\r\n", i))
	}

	offset := s.ScrollbackLen()

	highlighted := s.RenderAt(offset, "line-0")
	if !strings.Contains(highlighted, "\x1b[7") && !strings.Contains(highlighted, ";7") {
		t.Fatalf("expected line-0 to be highlighted at offset %d:\n%q", offset, highlighted)
	}

	again := s.RenderAt(offset, "")
	if strings.Contains(again, "\x1b[7") || strings.Contains(again, ";7") {
		t.Errorf("a later unhighlighted RenderAt of the same line still carries reverse video — stored scrollback was mutated:\n%q", again)
	}
}
