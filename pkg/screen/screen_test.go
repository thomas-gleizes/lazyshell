package screen

import (
	"strings"
	"testing"
)

func render(t *testing.T, cols, rows int, input string) string {
	t.Helper()

	s := New(cols, rows)

	if _, err := s.Write([]byte(input)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	return s.Render()
}

// The reason the whole rendering strategy changed: a themed prompt redraws
// itself by moving the cursor up and rewriting. Appending gave four stacked
// prompts; the emulator must show exactly one.
func TestInPlaceRedrawOverwrites(t *testing.T) {
	// Draw a prompt, then go back up and draw a different one, three times —
	// the shape a zsh theme emits at startup.
	input := "prompt-v1\r\n" +
		"\x1b[A\r\x1b[Kprompt-v2\r\n" +
		"\x1b[A\r\x1b[Kprompt-v3\r\n"

	out := render(t, 40, 10, input)

	if n := strings.Count(out, "prompt-v"); n != 1 {
		t.Errorf("%d prompts on screen, want 1:\n%s", n, out)
	}

	if !strings.Contains(out, "prompt-v3") {
		t.Errorf("the last redraw is missing:\n%s", out)
	}
}

// Erasing the screen must actually erase it.
func TestClearScreen(t *testing.T) {
	out := render(t, 40, 5, "avant\r\n\x1b[2J\x1b[H"+"après")

	if strings.Contains(out, "avant") {
		t.Errorf("ESC[2J did not clear the screen:\n%s", out)
	}

	if !strings.Contains(out, "après") {
		t.Errorf("text written after the clear is missing:\n%s", out)
	}
}

// Colours survive: they are what gocui can render, and the whole point of
// keeping SGR in the output.
func TestColoursArePreserved(t *testing.T) {
	out := render(t, 40, 5, "\x1b[31mrouge\x1b[0m")

	if !strings.Contains(out, "rouge") {
		t.Fatalf("text missing:\n%q", out)
	}

	if !strings.Contains(out, "\x1b[") {
		t.Errorf("no SGR sequence in the rendered output:\n%q", out)
	}
}

// The rendered screen is bounded by the geometry, whatever the volume of
// output. This is what stops a chatty session from freezing the UI.
func TestRenderSizeIsBounded(t *testing.T) {
	s := New(80, 24)

	for range 5000 {
		if _, err := s.Write([]byte("une ligne de sortie bien bavarde\r\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if got := strings.Count(s.Render(), "\n"); got > 24 {
		t.Errorf("rendered screen has %d lines, want at most 24", got+1)
	}

	if s.ScrollbackLen() == 0 {
		t.Error("nothing went to the scrollback")
	}
}

// A full-screen application announces itself, which the UI needs to know: it is
// the difference between "shell output" and "vim is in control".
func TestAltScreenIsReported(t *testing.T) {
	s := New(40, 10)

	if s.IsAltScreen() {
		t.Fatal("alt-screen active before anything was written")
	}

	if _, err := s.Write([]byte("\x1b[?1049h")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !s.IsAltScreen() {
		t.Error("entering the alternate screen was not detected")
	}
}

func TestResizeChangesGeometry(t *testing.T) {
	s := New(80, 24)
	s.Resize(40, 10)

	if got := strings.Count(s.Render(), "\n"); got > 10 {
		t.Errorf("screen still has %d lines after resizing to 10", got+1)
	}
}
