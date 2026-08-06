package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"
)

// newOutputTestGui builds a Gui with one real session selected and its
// output view already created (via two layout passes, same convention as
// layout_test.go), ready to drive through editOutput directly.
func newOutputTestGui(t *testing.T) (*Gui, *gocui.View) {
	t.Helper()

	gui, g := newHeadlessGui(t)

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

	return gui, view
}

func typeIntoOutput(gui *Gui, view *gocui.View, text string) {
	for _, ch := range text {
		gui.editOutput(view, 0, ch, gocui.ModNone)
	}
}

func pressOutputKey(gui *Gui, view *gocui.View, key gocui.Key) {
	gui.editOutput(view, key, 0, gocui.ModNone)
}

// waitForSessionScreen polls a session's rendered screen until it contains
// want — the shell's own timing is not otherwise deterministic.
func waitForSessionScreen(t *testing.T, gui *Gui, want string) {
	t.Helper()

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if out := sess.Screen().Render(); strings.Contains(out, want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q on screen:\n%s", want, sess.Screen().Render())
}

func TestEditOutputEntersPassThroughOnI(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")

	if !gui.passThroughActive {
		t.Error("'i' did not arm pass-through")
	}
}

func TestEditOutputEntersPassThroughOnEnter(t *testing.T) {
	gui, view := newOutputTestGui(t)

	pressOutputKey(gui, view, gocui.KeyEnter)

	if !gui.passThroughActive {
		t.Error("Enter did not arm pass-through")
	}
}

// The core of pass-through: once armed, ordinary keystrokes must actually
// reach the shell.
func TestEditOutputForwardsKeystrokesDuringPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i") // arm pass-through
	typeIntoOutput(gui, view, "echo lazyshell-ok")
	pressOutputKey(gui, view, gocui.KeyEnter)

	waitForSessionScreen(t, gui, "lazyshell-ok")
}

func TestEditOutputPrefixAloneExitsPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gui.prefixKey)
	typeIntoOutput(gui, view, "x") // anything other than the prefix disarms

	if gui.passThroughActive {
		t.Error("prefix followed by another key did not leave pass-through")
	}
}

func TestEditOutputDoubledPrefixStaysInPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gui.prefixKey)
	pressOutputKey(gui, view, gui.prefixKey)

	if !gui.passThroughActive {
		t.Error("a doubled prefix left pass-through, it should send a literal byte instead")
	}
}

func TestEditOutputScrollKeysAdjustOffsetOutsidePassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	// Produce enough scrollback to have somewhere to scroll to, then wait for
	// a trailing marker so the shell has fully caught up — otherwise
	// ScrollbackLen() keeps growing underneath the assertions below.
	sess := gui.selectedSession()
	if _, err := sess.Write([]byte("for i in $(seq 1 200); do echo une-ligne-bavarde; done; echo done-marker\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForSessionScreen(t, gui, "done-marker")

	if sess.Screen().ScrollbackLen() == 0 {
		t.Fatal("no scrollback accumulated")
	}

	pressOutputKey(gui, view, gocui.KeyPgup)
	if gui.getScrollOffset() <= 0 {
		t.Error("PgUp did not scroll back")
	}

	pressOutputKey(gui, view, gocui.KeyPgdn)
	pressOutputKey(gui, view, gocui.KeyPgdn)
	pressOutputKey(gui, view, gocui.KeyPgdn)
	if gui.getScrollOffset() != 0 {
		t.Errorf("PgDn past the bottom did not clamp at 0, got %d", gui.getScrollOffset())
	}

	max := sess.Screen().ScrollbackLen()
	for range 50 {
		pressOutputKey(gui, view, gocui.KeyPgup)
	}
	if got := gui.getScrollOffset(); got != max {
		t.Errorf("PgUp past the top did not clamp at ScrollbackLen() (%d), got %d", max, got)
	}
}

// Scroll keys must not be interpreted as scroll gestures while pass-through
// is active — they have to reach the shell instead.
func TestEditOutputIgnoresScrollKeysDuringPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i") // arm pass-through, resets offset to 0

	pressOutputKey(gui, view, gocui.KeyPgup)

	if gui.getScrollOffset() != 0 {
		t.Errorf("PgUp during pass-through changed the scroll offset to %d, want 0 (forwarded to the shell instead)", gui.getScrollOffset())
	}

	if !gui.passThroughActive {
		t.Error("PgUp during pass-through unexpectedly left pass-through")
	}
}

// A digit typed during pass-through must reach the shell like any other
// character — never be mistaken for the "1".."9" direct-jump keys, which are
// only ever registered on sessionsViewName and so structurally unreachable
// from here, but worth a test that documents and locks in the intent.
func TestEditOutputForwardsDigitsDuringPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	before := gui.getSelectedIndex()

	typeIntoOutput(gui, view, "i") // arm pass-through
	typeIntoOutput(gui, view, "echo 42\r")

	waitForSessionScreen(t, gui, "42")

	if got := gui.getSelectedIndex(); got != before {
		t.Errorf("selected index changed to %d during pass-through, want unchanged %d", got, before)
	}
}
