package gui

import (
	"errors"
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

// The wrapper must be free when --debug is off: setKeybindings runs it over
// every binding, and a closure per keystroke on a normal run would be a cost
// paid by everyone for a flag almost nobody passes.
func TestLoggedReturnsTheHandlerUntouchedWithoutDebug(t *testing.T) {
	var gui Gui

	called := false
	handler := func(*gocui.Gui, *gocui.View) error {
		called = true

		return nil
	}

	if err := gui.logged("quit", "", "q", handler)(nil, nil); err != nil {
		t.Fatalf("wrapped handler: %v", err)
	}

	if !called {
		t.Error("the handler was not called")
	}

	if gui.logged("quit", "", "q", nil) != nil {
		t.Error("logged(nil handler) returned something to register")
	}
}

func TestLoggedRecordsTheActionAndItsError(t *testing.T) {
	gui := newTestDebugGui(t)

	wantErr := errors.New("boom")
	wrapped := gui.logged("new_session", sessionsViewName, "n", func(*gocui.Gui, *gocui.View) error {
		return wantErr
	})

	if err := wrapped(nil, nil); !errors.Is(err, wantErr) {
		t.Fatalf("wrapped handler returned %v, want the handler's own error", err)
	}

	entries := gui.debug.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want the call and its error: %v", len(entries), entries)
	}

	if !strings.Contains(entries[0].Text, "new_session [n on sessions]") {
		t.Errorf("call line = %q, want the action, key and view", entries[0].Text)
	}

	if !strings.Contains(entries[1].Text, "boom") {
		t.Errorf("error line = %q, want the returned error", entries[1].Text)
	}
}

// An empty ViewName is a global binding, and "global" is more useful in a log
// than a blank.
func TestLoggedNamesGlobalBindings(t *testing.T) {
	gui := newTestDebugGui(t)

	wrapped := gui.logged("help", "", "?", func(*gocui.Gui, *gocui.View) error { return nil })
	_ = wrapped(nil, nil)

	entries := gui.debug.Recent(10)
	if len(entries) != 1 || !strings.Contains(entries[0].Text, "on global") {
		t.Errorf("entries = %v, want one line naming the global scope", entries)
	}
}

func TestDebugKeyName(t *testing.T) {
	for _, c := range []struct {
		name string
		key  gocui.Key
		ch   rune
		mod  gocui.Modifier
		want string
	}{
		// A printable character arrives as key 0 with the rune set, which is
		// what ch != 0 selects on.
		{"printable", 0, 'a', gocui.ModNone, "a"},
		{"control key", gocui.KeyCtrlO, 0, gocui.ModNone, "Ctrl-O"},
		{"named key", gocui.KeyArrowDown, 0, gocui.ModNone, "↓"},
		{"alt letter", 0, 'j', gocui.ModAlt, "Alt+j"},
		{"function key", gocui.KeyF12, 0, gocui.ModNone, "F12"},
	} {
		if got := debugKeyName(c.key, c.ch, c.mod); got != c.want {
			t.Errorf("%s: debugKeyName = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMouseKeyName(t *testing.T) {
	for _, c := range []struct {
		key  gocui.Key
		mod  gocui.Modifier
		want string
	}{
		{gocui.MouseLeft, gocui.ModNone, "left"},
		{gocui.MouseLeft, gocui.ModMotion, "left+drag"},
		{gocui.MouseWheelUp, gocui.ModNone, "wheel-up"},
		{gocui.MouseWheelDown, gocui.ModNone, "wheel-down"},
	} {
		if got := mouseKeyName(c.key, c.mod); got != c.want {
			t.Errorf("mouseKeyName(%v, %v) = %q, want %q", c.key, c.mod, got, c.want)
		}
	}
}

// The raw event and what Normalize made of it are logged together, because the
// interesting failures are exactly the ones where the two disagree — a Ctrl
// modifier folded into a control key here.
func TestLogKeyEventShowsBothFormsWhenNormalizeChangesThem(t *testing.T) {
	gui := newTestDebugGui(t)

	gui.logKeyEvent('o', 'o', gocui.Modifier(2))

	entries := gui.debug.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}

	if !strings.Contains(entries[0].Text, "→") {
		t.Errorf("line = %q, want the normalized form after an arrow", entries[0].Text)
	}
}

func TestLogKeyEventShowsOneFormWhenNothingChanges(t *testing.T) {
	gui := newTestDebugGui(t)

	gui.logKeyEvent(0, 'a', gocui.ModNone)

	entries := gui.debug.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}

	if strings.Contains(entries[0].Text, "→") {
		t.Errorf("line = %q, want no arrow when Normalize left the event alone", entries[0].Text)
	}

	if !strings.Contains(entries[0].Text, "[scroll]") {
		t.Errorf("line = %q, want the input mode it landed in", entries[0].Text)
	}
}

func TestInputModeName(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*Gui)
		want  string
	}{
		{"default", func(*Gui) {}, "scroll"},
		{"pass-through", func(g *Gui) { g.passThroughActive = true }, "pass-through"},
		{"secondary tab", func(g *Gui) { g.outputTab = tabResources }, "tab:resources"},
		{"copy mode", func(g *Gui) { g.copyModeActive = true }, "copy-mode"},
		// searchActive() needs matches too, not just a pattern.
		{"search", func(g *Gui) {
			g.searchPattern = "x"
			g.searchMatches = []int{3}
		}, "search"},
		// Pass-through wins: it is the first thing editOutput tests.
		{"pass-through over copy", func(g *Gui) {
			g.passThroughActive = true
			g.copyModeActive = true
		}, "pass-through"},
	} {
		var gui Gui

		c.setup(&gui)

		if got := gui.inputModeName(); got != c.want {
			t.Errorf("%s: inputModeName = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOutputTabString(t *testing.T) {
	for tab, want := range map[outputTab]string{
		tabTerminal:    "terminal",
		tabResources:   "resources",
		tabEnvironment: "environment",
		outputTab(9):   "tab(9)",
	} {
		if got := tab.String(); got != want {
			t.Errorf("outputTab(%d).String() = %q, want %q", int(tab), got, want)
		}
	}
}
