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

	// New defaults to pass-through armed (docs/adr/0011-passthrough-par-defaut.md);
	// this file's tests are about the locked→armed transition itself, so they
	// need a known locked starting point.
	gui.exitPassThrough()

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
	typeIntoOutput(gui, view, "echo lazyshell-''ok")
	pressOutputKey(gui, view, gocui.KeyEnter)

	waitForSessionScreen(t, gui, "lazyshell-ok")
}

// One press is the whole contract: no second key confirms it, nothing is left
// armed afterwards. This is what the old two-step automaton got wrong — see
// editDuringPassThrough.
func TestEditOutputPrefixAloneExitsPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gui.prefixKey)

	if gui.passThroughActive {
		t.Error("a single press of the escape key did not leave pass-through")
	}
}

// The second exit (ADR 0005): two Escapes in a row, no prefix key to know.
func TestEditOutputDoubleEscExitsPassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gocui.KeyEsc)

	if !gui.passThroughActive {
		t.Fatal("a single Esc left pass-through, it must reach the session instead")
	}

	pressOutputKey(gui, view, gocui.KeyEsc)

	if gui.passThroughActive {
		t.Error("Esc Esc did not leave pass-through")
	}
}

// The pair has to be a deliberate double press. Two Escapes minutes apart are
// two unrelated keystrokes, and treating them as an exit would bring back the
// never-expiring armed state ADR 0004 removed.
func TestEditOutputStaleEscDoesNotPairUp(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")

	gui.lastEscAt = time.Now().Add(-escExitWindow - time.Second)
	pressOutputKey(gui, view, gocui.KeyEsc)

	if !gui.passThroughActive {
		t.Error("an Esc paired with a long-expired one and left pass-through")
	}
}

// Esc, some other key, Esc is a vim user moving around, not someone asking to
// leave: any key in between breaks the pair.
func TestEditOutputKeyBetweenEscapesBreaksThePair(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gocui.KeyEsc)
	typeIntoOutput(gui, view, "j")
	pressOutputKey(gui, view, gocui.KeyEsc)

	if !gui.passThroughActive {
		t.Error("Esc j Esc left pass-through, the intervening key must break the pair")
	}
}

// Entering pass-through starts with no pair in progress: an Escape typed in an
// earlier pass-through must not complete with the first one typed here.
func TestEditOutputEscPairDoesNotSurviveReentry(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gocui.KeyEsc)
	pressOutputKey(gui, view, gui.prefixKey)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gocui.KeyEsc)

	if !gui.passThroughActive {
		t.Error("an Esc from a previous pass-through paired with the first of this one")
	}
}

// The key that used to be the escape prefix must now reach the shell like any
// other: Claude Code binds Ctrl-B, and getting it through is half the reason
// the default moved to Ctrl-O.
func TestEditOutputForwardsFormerPrefixToSession(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "i")
	pressOutputKey(gui, view, gocui.KeyCtrlB)

	if !gui.passThroughActive {
		t.Error("Ctrl-B left pass-through, it must be forwarded to the session now")
	}
}

// A printable character must never be mistaken for the escape key, whatever
// the escape key is: they arrive with key 0 and the rune set, and a prefix of
// key 0 (Ctrl-Space, NUL for tcell) would otherwise match every one of them.
func TestEditOutputPrintableKeyIsNotTheEscapeKey(t *testing.T) {
	gui, view := newOutputTestGui(t)

	gui.prefixKey = 0

	typeIntoOutput(gui, view, "i")
	typeIntoOutput(gui, view, "a")

	if !gui.passThroughActive {
		t.Error("a printable key was taken for the escape key and left pass-through")
	}
}

func TestEditOutputScrollKeysAdjustOffsetOutsidePassThrough(t *testing.T) {
	gui, view := newOutputTestGui(t)

	// Produce enough scrollback to have somewhere to scroll to, then wait for
	// a trailing marker so the shell has fully caught up — otherwise
	// ScrollbackLen() keeps growing underneath the assertions below.
	//
	// The marker is written as done-''marker so the shell prints "done-marker"
	// while the *typed* line, which the pty echoes back onto the screen, does
	// not contain it. Without that split the wait below returns on the echo,
	// before a single line of output exists, and the assertions run against an
	// empty scrollback — the same trap hook_test.go's readSockEnv documents.
	sess := gui.selectedSession()
	if _, err := sess.Write([]byte("for i in $(seq 1 200); do echo une-ligne-bavarde; done; echo done-''marker\n")); err != nil {
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
	typeIntoOutput(gui, view, "echo 4''2\r")

	waitForSessionScreen(t, gui, "42")

	if got := gui.getSelectedIndex(); got != before {
		t.Errorf("selected index changed to %d during pass-through, want unchanged %d", got, before)
	}
}
