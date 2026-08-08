package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
)

// currentViewName is the question every test here asks: which panel owns the
// keyboard now.
func currentViewName(gui *Gui) string {
	current := gui.g.CurrentView()
	if current == nil {
		return ""
	}

	return current.Name()
}

// → is Tab's directional half: it moves to the panel drawn on the right.
func TestArrowRightFocusesOutputPanel(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if _, err := gui.g.SetCurrentView(sessionsViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	if err := gui.focusOutputPanel(gui.g, nil); err != nil {
		t.Fatalf("focusOutputPanel: %v", err)
	}

	if got := currentViewName(gui); got != outputViewName {
		t.Errorf("current view after → = %q, want %q", got, outputViewName)
	}

	// A navigation gesture, not "start typing": pass-through stays off, so
	// the next keystroke still belongs to lazyshell.
	if gui.passThroughActive {
		t.Error("→ armed pass-through, want it left off")
	}
}

// The output panel is not a focus target with nothing to show — same rule
// cycleFocus already follows.
func TestArrowRightWithoutSessionStaysPut(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.focusOutputPanel(g, nil); err != nil {
		t.Fatalf("focusOutputPanel: %v", err)
	}

	if got := currentViewName(gui); got == outputViewName {
		t.Error("→ focused the output panel with no session selected")
	}
}

// ← comes back, and it goes through the Editor rather than a keybinding —
// that path is the one the user actually presses.
func TestArrowLeftFromOutputFocusesSessions(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	view, err := gui.g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	if !gui.editOutput(view, gocui.KeyArrowLeft, 0, gocui.ModNone) {
		t.Fatal("editOutput did not claim ←")
	}

	if got := currentViewName(gui); got != sessionsViewName {
		t.Errorf("current view after ← = %q, want %q", got, sessionsViewName)
	}
}

// The load-bearing exclusion: during pass-through ← is the shell's key, and
// stealing it would break every readline and vim binding built on it.
func TestArrowLeftReachesShellDuringPassThrough(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	view, err := gui.g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	gui.enterPassThrough()

	gui.editOutput(view, gocui.KeyArrowLeft, 0, gocui.ModNone)

	if got := currentViewName(gui); got != outputViewName {
		t.Errorf("← left the output panel during pass-through (now %q)", got)
	}

	if !gui.passThroughActive {
		t.Error("← disarmed pass-through")
	}
}

// While selecting, the arrows belong to the selection.
func TestArrowLeftDoesNotLeavePanelInCopyMode(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	view, err := gui.g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	gui.enterCopyMode()

	gui.editOutput(view, gocui.KeyArrowLeft, 0, gocui.ModNone)

	if got := currentViewName(gui); got != outputViewName {
		t.Errorf("← left the output panel in copy mode (now %q)", got)
	}

	if !gui.copyModeActive {
		t.Error("← cancelled copy mode")
	}
}

// The resources/env tabs are not terminals, so ← is free there too.
func TestArrowLeftOnSecondaryTabFocusesSessions(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	view, err := gui.g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
		t.Fatalf("SetCurrentView: %v", err)
	}

	gui.outputTab = tabResources

	if !gui.editOutput(view, gocui.KeyArrowLeft, 0, gocui.ModNone) {
		t.Fatal("editOutput did not claim ← on the resources tab")
	}

	if got := currentViewName(gui); got != sessionsViewName {
		t.Errorf("current view after ← = %q, want %q", got, sessionsViewName)
	}
}

// Zoomed, the sessions panel does not exist: ← has nowhere to go and must not
// error out or blank the focus.
func TestArrowLeftWhileZoomedStaysOnOutput(t *testing.T) {
	gui := newSessionsErgonomicsTestGui(t)

	if err := gui.toggleZoom(gui.g, nil); err != nil {
		t.Fatalf("toggleZoom: %v", err)
	}

	for range 2 {
		if err := gui.layout(gui.g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := gui.g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	gui.editOutput(view, gocui.KeyArrowLeft, 0, gocui.ModNone)

	if got := currentViewName(gui); got != outputViewName {
		t.Errorf("current view after ← while zoomed = %q, want %q", got, outputViewName)
	}
}
