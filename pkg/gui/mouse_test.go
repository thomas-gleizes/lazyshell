package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// There is no way to inject events into gocui's main loop from a test — the
// convention in this package is to call handlers directly, so these build the
// ViewMouseBindingOpts gocui would have built and hand them over.

// TestEditOutputDropsMouseKeys is the central regression guard of the whole
// mouse feature. gocui gives MouseLeft and KeyShiftArrowDown the same value and
// routes unclaimed mouse events through the output view's Editor, so without
// the guard at the top of editOutput a click during pass-through is typed into
// the shell as "\x1b[1;2B".
func TestEditOutputDropsMouseKeys(t *testing.T) {
	gui, view := newOutputTestGui(t)
	gui.passThroughActive = true

	for _, key := range []gocui.Key{
		gocui.MouseLeft,
		gocui.MouseRight,
		gocui.MouseMiddle,
		gocui.MouseRelease,
		gocui.MouseWheelUp,
		gocui.MouseWheelDown,
	} {
		if gui.editOutput(view, key, 0, gocui.ModNone) {
			t.Errorf("editOutput claimed mouse key %v; it must fall through to the mouse bindings", key)
		}
	}
}

// TestEditOutputKeepsShiftArrowsWhenMouseOff is the other half of the bargain:
// the ambiguity is resolved in the mouse's favour only while the mouse is on.
// Turn it off and Shift-Down must reach the session again.
func TestEditOutputKeepsShiftArrowsWhenMouseOff(t *testing.T) {
	cfg := config.Default()
	cfg.Mouse.Enabled = false

	gui, g := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	if _, err := gui.sessions.New("t", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	gui.passThroughActive = true

	if !gui.editOutput(view, gocui.KeyShiftArrowDown, 0, gocui.ModNone) {
		t.Error("Shift-Down was dropped even though the mouse is disabled")
	}
}

func TestClickSessionSelectsThatLine(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for _, name := range []string{"a", "b", "c"} {
		if _, err := gui.sessions.New(name, "/bin/sh"); err != nil {
			t.Fatalf("sessions.New: %v", err)
		}
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.clickSession(gocui.ViewMouseBindingOpts{Y: 2}); err != nil {
		t.Fatalf("clickSession: %v", err)
	}

	if got := gui.getSelectedIndex(); got != 2 {
		t.Errorf("selected index = %d, want 2", got)
	}
}

// A single click is navigation, not engagement — the same rule j/k follow.
func TestClickSessionDoesNotArmPassThrough(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if _, err := gui.sessions.New("a", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.clickSession(gocui.ViewMouseBindingOpts{Y: 0}); err != nil {
		t.Fatalf("clickSession: %v", err)
	}

	if gui.passThroughActive {
		t.Error("a single click armed pass-through; only a double click should")
	}

	if err := gui.clickSession(gocui.ViewMouseBindingOpts{Y: 0, IsDoubleClick: true}); err != nil {
		t.Fatalf("clickSession: %v", err)
	}

	if !gui.passThroughActive {
		t.Error("a double click did not focus the shell")
	}
}

func TestClickSessionOutOfRangeIsNoOp(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if _, err := gui.sessions.New("a", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	if _, err := gui.sessions.New("b", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.applySelection(1); err != nil {
		t.Fatalf("applySelection: %v", err)
	}

	// Past the end of the list, and the "no sessions" placeholder line's
	// position on an empty one.
	for _, y := range []int{2, 7, -1} {
		if err := gui.clickSession(gocui.ViewMouseBindingOpts{Y: y}); err != nil {
			t.Fatalf("clickSession(%d): %v", y, err)
		}

		if got := gui.getSelectedIndex(); got != 1 {
			t.Errorf("click at y=%d changed the selection to %d, want it left at 1", y, got)
		}
	}
}

func TestClickSessionOnEmptyListIsNoOp(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.clickSession(gocui.ViewMouseBindingOpts{Y: 0}); err != nil {
		t.Fatalf("clickSession on an empty list: %v", err)
	}
}

// The requirement that motivated the feature: a wheel notch moves lazyshell's
// own scrollback and never becomes an arrow key, which at a shell prompt would
// recall the previous command instead of scrolling.
func TestWheelOutputScrollsTheScrollback(t *testing.T) {
	gui, _ := newOutputTestGui(t)

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	// Give the scrollback something to scroll into.
	for range 100 {
		if _, err := sess.Screen().Write([]byte("line\r\n")); err != nil {
			t.Fatalf("screen write: %v", err)
		}
	}

	if err := gui.wheelOutput(1)(gocui.ViewMouseBindingOpts{}); err != nil {
		t.Fatalf("wheel up: %v", err)
	}

	up := gui.getScrollOffset()
	if up != gui.wheelLines() {
		t.Errorf("scroll offset after one notch up = %d, want %d", up, gui.wheelLines())
	}

	if err := gui.wheelOutput(-1)(gocui.ViewMouseBindingOpts{}); err != nil {
		t.Fatalf("wheel down: %v", err)
	}

	if got := gui.getScrollOffset(); got != 0 {
		t.Errorf("scroll offset back down = %d, want 0", got)
	}
}

// On the alternate screen there is no scrollback behind the application, so a
// notch does nothing rather than scrolling something that is not there.
func TestWheelOutputIsNoOpOnAltScreen(t *testing.T) {
	gui, _ := newOutputTestGui(t)

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	if _, err := sess.Screen().Write([]byte("\x1b[?1049h")); err != nil {
		t.Fatalf("screen write: %v", err)
	}

	if err := gui.wheelOutput(1)(gocui.ViewMouseBindingOpts{}); err != nil {
		t.Fatalf("wheel up: %v", err)
	}

	if got := gui.getScrollOffset(); got != 0 {
		t.Errorf("scroll offset = %d on the alternate screen, want 0", got)
	}
}

func TestWheelLinesFallsBackOnZero(t *testing.T) {
	gui := New(nil, config.Config{})

	if got := gui.wheelLines(); got != defaultWheelLines {
		t.Errorf("wheelLines() = %d on a zero config, want %d", got, defaultWheelLines)
	}
}

func TestDragOutputSelectsARange(t *testing.T) {
	gui, _ := newOutputTestGui(t)

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	for range 50 {
		if _, err := sess.Screen().Write([]byte("line\r\n")); err != nil {
			t.Fatalf("screen write: %v", err)
		}
	}

	anchor, ok := gui.outputLineAt(2)
	if !ok {
		t.Fatal("outputLineAt(2) failed")
	}

	if err := gui.dragOutput(gocui.ViewMouseBindingOpts{Y: 2}); err != nil {
		t.Fatalf("drag start: %v", err)
	}

	if !gui.copyModeActive {
		t.Fatal("dragging did not enter copy-mode")
	}

	if gui.copyAnchorLine != anchor {
		t.Errorf("anchor = %d, want %d (the line the button went down on)", gui.copyAnchorLine, anchor)
	}

	if err := gui.dragOutput(gocui.ViewMouseBindingOpts{Y: 5}); err != nil {
		t.Fatalf("drag extend: %v", err)
	}

	from, to := gui.copySelectionRange()
	if from != anchor || to != anchor+3 {
		t.Errorf("selection = [%d, %d], want [%d, %d]", from, to, anchor, anchor+3)
	}

	// Releasing copies nothing on its own: the selection is still there,
	// waiting for "y".
	if !gui.copyModeActive {
		t.Error("the selection was dropped before the user could yank it")
	}
}

// forwardMouseToApp is the only path that can put a mouse event on a pty, and
// it must stay shut for a program that never asked for the mouse — a shell, an
// AI agent CLI. That is what keeps the wheel scrolling the buffer.
func TestForwardMouseToAppRefusesWhenAppDidNotAsk(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	gui.passThroughActive = true

	if gui.appWantsMouse() {
		t.Error("appWantsMouse() is true although nothing armed a tracking mode")
	}

	if gui.forwardMouseToApp(gocui.MouseWheelUp, gocui.ModNone, gocui.ViewMouseBindingOpts{}) {
		t.Error("a wheel notch was forwarded to a program that never asked for the mouse")
	}
}

func TestForwardMouseToAppAcceptsOnceTheAppAsks(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	gui.passThroughActive = true

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	if _, err := sess.Screen().Write([]byte("\x1b[?1002h\x1b[?1006h")); err != nil {
		t.Fatalf("screen write: %v", err)
	}

	if !gui.appWantsMouse() {
		t.Fatal("appWantsMouse() is false after the application set DECSET 1002")
	}

	if !gui.forwardMouseToApp(gocui.MouseLeft, gocui.ModNone, gocui.ViewMouseBindingOpts{X: 4, Y: 2}) {
		t.Error("a click was not forwarded although the application asked for the mouse")
	}
}

// Outside pass-through the panel is being navigated, not typed into, so the
// mouse belongs to lazyshell whatever the program armed.
func TestForwardMouseToAppRefusesOutsidePassThrough(t *testing.T) {
	gui, _ := newOutputTestGui(t)

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	if _, err := sess.Screen().Write([]byte("\x1b[?1000h")); err != nil {
		t.Fatalf("screen write: %v", err)
	}

	if gui.forwardMouseToApp(gocui.MouseLeft, gocui.ModNone, gocui.ViewMouseBindingOpts{}) {
		t.Error("a click was forwarded while the panel was only being navigated")
	}
}

func TestSetMouseBindingsRegistersNothingWhenDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Mouse.Enabled = false

	gui, g := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	if err := gui.setKeybindings(g); err != nil {
		t.Fatalf("setKeybindings: %v", err)
	}
}

func TestMouseBindingsAllHaveHandlers(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for _, b := range gui.mouseBindings() {
		if b.Handler == nil {
			t.Errorf("mouse binding %v on %q has no handler", b.Key, b.ViewName)
		}
	}

	if err := gui.setKeybindings(g); err != nil {
		t.Fatalf("setKeybindings: %v", err)
	}
}
