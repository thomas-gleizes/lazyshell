package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
)

// newPromptTestGui builds a Gui with the sessions/output/status views already
// created (two layout passes, same convention as layout_test.go), ready to
// drive showPrompt directly.
func newPromptTestGui(t *testing.T) (*Gui, *gocui.Gui) {
	t.Helper()

	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	return gui, g
}

func TestShowPromptOpensPopupPreFilled(t *testing.T) {
	gui, g := newPromptTestGui(t)

	if err := gui.showPrompt("titre", "valeur initiale", func(string) error { return nil }); err != nil {
		t.Fatalf("showPrompt: %v", err)
	}

	view, err := g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	if got := view.Buffer(); got != "valeur initiale" {
		t.Errorf("Buffer() = %q, want %q", got, "valeur initiale")
	}

	if cx, _ := view.Cursor(); cx != len("valeur initiale") {
		t.Errorf("cursor x = %d, want %d (end of initial text)", cx, len("valeur initiale"))
	}

	if current := g.CurrentView(); current == nil || current.Name() != promptViewName {
		t.Errorf("current view = %v, want %q", current, promptViewName)
	}
}

func TestSubmitPromptCallsOnSubmitWithTrimmedText(t *testing.T) {
	gui, g := newPromptTestGui(t)

	var got string
	called := false
	if err := gui.showPrompt("titre", "", func(s string) error {
		called = true
		got = s

		return nil
	}); err != nil {
		t.Fatalf("showPrompt: %v", err)
	}

	view, err := g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()
	view.SetOrigin(0, 0)
	if _, err := view.Write([]byte("  hello  ")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := gui.submitPrompt(g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	if !called {
		t.Fatal("onSubmit was not called")
	}
	if got != "hello" {
		t.Errorf("onSubmit received %q, want trimmed %q", got, "hello")
	}

	if _, err := g.View(promptViewName); err == nil {
		t.Error("prompt view still exists after submit")
	}
}

func TestSubmitPromptClearsPendingCallback(t *testing.T) {
	gui, g := newPromptTestGui(t)

	calls := 0
	if err := gui.showPrompt("titre", "x", func(string) error {
		calls++

		return nil
	}); err != nil {
		t.Fatalf("showPrompt: %v", err)
	}

	view, err := g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}

	if err := gui.submitPrompt(g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}
	if calls != 1 {
		t.Fatalf("onSubmit called %d times, want 1", calls)
	}

	if gui.promptOnSubmit != nil {
		t.Error("promptOnSubmit was not cleared after submit")
	}
}

func TestCancelPromptDoesNotCallOnSubmit(t *testing.T) {
	gui, g := newPromptTestGui(t)

	called := false
	if err := gui.showPrompt("titre", "x", func(string) error {
		called = true

		return nil
	}); err != nil {
		t.Fatalf("showPrompt: %v", err)
	}

	if err := gui.cancelPrompt(g, nil); err != nil {
		t.Fatalf("cancelPrompt: %v", err)
	}

	if called {
		t.Error("cancelPrompt called onSubmit, want it left uncalled")
	}
	if _, err := g.View(promptViewName); err == nil {
		t.Error("prompt view still exists after cancelPrompt")
	}
	if current := g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view after cancel = %v, want %q", current, sessionsViewName)
	}
}
