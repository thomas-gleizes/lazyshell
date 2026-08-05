package screen

import (
	"fmt"
	"strings"
	"testing"
	"time"
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

// Read must never hold the lock: a session's drain goroutine calls it in a
// loop (nothing ever writes an answer in this test, since none was queried),
// and a Write happening concurrently must not be blocked by it.
func TestReadDoesNotBlockWrite(t *testing.T) {
	s := New(40, 10)

	go func() {
		buf := make([]byte, 16)
		_, _ = s.Read(buf) //nolint:errcheck // blocks until Close, error is expected then
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Write([]byte("hi"))
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Write blocked behind a pending Read")
	}
}

// Closing the screen must release a goroutine parked in Read — the only way
// to do so, since Read blocks on an in-memory pipe unrelated to the pty fd.
func TestCloseUnblocksRead(t *testing.T) {
	s := New(40, 10)

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 16)
		_, _ = s.Read(buf)
	}()

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock the pending Read")
	}
}

func TestRenderAtZeroMatchesRender(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		if _, err := s.Write([]byte(fmt.Sprintf("line-%d\r\n", i))); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if got, want := s.RenderAt(0), s.Render(); got != want {
		t.Errorf("RenderAt(0) = %q, want %q", got, want)
	}

	if got := s.RenderAt(-1); got != s.Render() {
		t.Errorf("RenderAt(-1) = %q, want the live view", got)
	}
}

// Scrolling back must actually show older content that has already left the
// live screen — the whole point of a scrollback viewport.
func TestRenderAtShowsEarlierContent(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		if _, err := s.Write([]byte(fmt.Sprintf("line-%d\r\n", i))); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	live := s.Render()
	if strings.Contains(live, "line-0") {
		t.Fatal("line-0 should already have scrolled off the live screen")
	}

	scrolled := s.RenderAt(s.ScrollbackLen())
	if scrolled == live {
		t.Fatal("RenderAt with a non-zero offset returned the same content as the live view")
	}

	if !strings.Contains(scrolled, "line-0") {
		t.Errorf("scrolled to the top but line-0 is missing:\n%s", scrolled)
	}
}

// An offset beyond the available history must not panic; it just stops at
// the oldest line kept.
func TestRenderAtClampsToScrollbackLen(t *testing.T) {
	s := New(40, 5)

	for i := range 20 {
		if _, err := s.Write([]byte(fmt.Sprintf("line-%d\r\n", i))); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	atMax := s.RenderAt(s.ScrollbackLen())
	beyond := s.RenderAt(s.ScrollbackLen() * 100)

	if atMax != beyond {
		t.Errorf("RenderAt beyond ScrollbackLen() = %q, want the same as at the max offset %q", beyond, atMax)
	}
}
