package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

// newCopyModeTestGui builds a Gui with one session fed deterministic content
// straight into its emulator (feed, from fullscreen_test.go) rather than a
// real shell — copy-mode's line arithmetic needs known content, not real
// timing.
func newCopyModeTestGui(t *testing.T) (*Gui, *gocui.View) {
	t.Helper()

	gui, g := newHeadlessGui(t)
	sess := newTestSession(t, gui, "t")

	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "line-%d\r\n", i)
	}
	feed(t, sess, b.String())

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	// New defaults it armed (ADR 0011); copy mode ('v') is only reachable
	// through editDuringScroll, i.e. once locked.
	gui.exitPassThrough()

	return gui, view
}

func TestVEntersCopyModeAtTheWindowsTopLine(t *testing.T) {
	gui, view := newCopyModeTestGui(t)
	sess := gui.selectedSession()

	gui.setScrollOffset(5)

	typeIntoOutput(gui, view, "v")

	if !gui.copyModeActive {
		t.Fatal("'v' did not enter copy mode")
	}

	want := sess.Screen().ScrollbackLen() - 5
	if gui.copyAnchorLine != want || gui.copyCursorLine != want {
		t.Errorf("anchor/cursor = %d/%d, want both %d", gui.copyAnchorLine, gui.copyCursorLine, want)
	}
}

func TestVIsANoopOnAnEmptySessionList(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	gui.enterCopyMode()

	if gui.copyModeActive {
		t.Error("entering copy mode with no session selected should be a no-op")
	}
}

func TestCopyModeMovementExtendsAndShrinksSelection(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	typeIntoOutput(gui, view, "v")
	anchor := gui.copyAnchorLine

	typeIntoOutput(gui, view, "jjj")

	if gui.copyCursorLine != anchor+3 {
		t.Errorf("cursor after 3 'j' = %d, want %d", gui.copyCursorLine, anchor+3)
	}

	if from, to := gui.copySelectionRange(); from != anchor || to != anchor+3 {
		t.Errorf("copySelectionRange() = (%d, %d), want (%d, %d)", from, to, anchor, anchor+3)
	}

	typeIntoOutput(gui, view, "kk")

	if gui.copyCursorLine != anchor+1 {
		t.Errorf("cursor after 2 'k' = %d, want %d", gui.copyCursorLine, anchor+1)
	}
}

func TestCopyModeArrowKeysAlsoMoveTheCursor(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	typeIntoOutput(gui, view, "v")
	anchor := gui.copyCursorLine

	pressOutputKey(gui, view, gocui.KeyArrowDown)
	pressOutputKey(gui, view, gocui.KeyArrowDown)

	if gui.copyCursorLine != anchor+2 {
		t.Errorf("cursor after 2 ArrowDown = %d, want %d", gui.copyCursorLine, anchor+2)
	}

	pressOutputKey(gui, view, gocui.KeyArrowUp)

	if gui.copyCursorLine != anchor+1 {
		t.Errorf("cursor after 1 ArrowUp = %d, want %d", gui.copyCursorLine, anchor+1)
	}
}

func TestCopyModeCursorNeverGoesNegative(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	gui.setScrollOffset(0)
	typeIntoOutput(gui, view, "v")

	for range 5 {
		typeIntoOutput(gui, view, "k")
	}

	if gui.copyCursorLine < 0 {
		t.Errorf("copyCursorLine = %d, want it clamped at 0", gui.copyCursorLine)
	}
}

func TestCopyModeEscCancelsWithoutCopying(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	typeIntoOutput(gui, view, "v")
	if !gui.copyModeActive {
		t.Fatal("copy mode did not arm")
	}

	pressOutputKey(gui, view, gocui.KeyEsc)

	if gui.copyModeActive {
		t.Error("Esc did not cancel copy mode")
	}
}

func TestCopyModeYankCopiesTheSelectionAndExits(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	tmpFile := filepath.Join(t.TempDir(), "clip.txt")
	gui.clipboardFallback = "cat > " + tmpFile

	typeIntoOutput(gui, view, "v")
	typeIntoOutput(gui, view, "jj") // three lines selected: anchor, +1, +2
	typeIntoOutput(gui, view, "y")

	if gui.copyModeActive {
		t.Error("'y' did not leave copy mode")
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("reading fallback clipboard file: %v", err)
	}

	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Errorf("clipboard content has %d newlines, want 2 (three lines)", got)
	}
}

func TestCopyModeSecondVAlsoYanks(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	tmpFile := filepath.Join(t.TempDir(), "clip.txt")
	gui.clipboardFallback = "cat > " + tmpFile

	typeIntoOutput(gui, view, "v")
	typeIntoOutput(gui, view, "v")

	if gui.copyModeActive {
		t.Error("a second 'v' did not leave copy mode")
	}

	if _, err := os.Stat(tmpFile); err != nil {
		t.Errorf("second 'v' did not yank: %v", err)
	}
}

func TestCopyModeDoesNotEnterOnAltScreen(t *testing.T) {
	gui, view := newCopyModeTestGui(t)
	sess := gui.selectedSession()

	feed(t, sess, "\x1b[?1049h") // enter alternate screen

	typeIntoOutput(gui, view, "v")

	if gui.copyModeActive {
		t.Error("'v' entered copy mode over a full-screen application")
	}
}

func TestOutputFooterShowsCopyModeHintsWhileActive(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	typeIntoOutput(gui, view, "v")

	hints := gui.outputFooterHints()
	if !containsKey(hints, "y") {
		t.Errorf("outputFooterHints() while in copy mode = %v, want a 'y' hint", hints)
	}
	if !containsKey(hints, "Esc") {
		t.Errorf("outputFooterHints() while in copy mode = %v, want an 'Esc' hint", hints)
	}
}

func TestStatusShowsCopyModeSelectionCount(t *testing.T) {
	gui, view := newCopyModeTestGui(t)

	typeIntoOutput(gui, view, "v")
	typeIntoOutput(gui, view, "jj")

	statusView, err := gui.g.View(statusViewName)
	if err != nil {
		t.Fatalf("status view not found: %v", err)
	}
	gui.renderStatus(statusView)

	if !strings.Contains(statusView.Buffer(), "3") {
		t.Errorf("status bar = %q, want it to mention the 3 selected lines", statusView.Buffer())
	}
}
