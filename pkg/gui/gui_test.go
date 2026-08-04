package gui

import (
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

// newHeadlessGui builds a gocui instance that renders to an in-memory screen,
// so the layout can be exercised without a real terminal.
func newHeadlessGui(t *testing.T) (*Gui, *gocui.Gui) {
	t.Helper()

	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode: gocui.OutputNormal,
		Headless:   true,
		Width:      80,
		Height:     24,
	})
	if err != nil {
		t.Fatalf("NewGui: %v", err)
	}
	t.Cleanup(g.Close)

	gui := New()
	gui.g = g

	return gui, g
}

func TestLayoutCreatesMainView(t *testing.T) {
	gui, g := newHeadlessGui(t)

	// Called twice: the first call creates the view, the second one only
	// resizes it. Both must succeed — gocui reports the creation through a
	// wrapped ErrUnknownView, which is easy to mishandle.
	for i := range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout call %d: %v", i+1, err)
		}
	}

	view, err := g.View(mainViewName)
	if err != nil {
		t.Fatalf("view %q not found: %v", mainViewName, err)
	}

	if !strings.Contains(view.Buffer(), "lazyshell") {
		t.Errorf("main view buffer does not contain the welcome text: %q", view.Buffer())
	}

	if current := g.CurrentView(); current == nil || current.Name() != mainViewName {
		t.Errorf("current view = %v, want %q", current, mainViewName)
	}
}

// A terminal too small to hold a bordered view must not make the layout fail.
func TestLayoutTinyTerminal(t *testing.T) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode: gocui.OutputNormal,
		Headless:   true,
		Width:      1,
		Height:     1,
	})
	if err != nil {
		t.Fatalf("NewGui: %v", err)
	}
	t.Cleanup(g.Close)

	gui := New()
	gui.g = g

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}
}

func TestSetKeybindings(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.setKeybindings(g); err != nil {
		t.Fatalf("setKeybindings: %v", err)
	}

	for _, b := range gui.bindings() {
		if b.Handler == nil {
			t.Errorf("binding %v has no handler", b.Key)
		}
		if b.Description == "" {
			t.Errorf("binding %v has no description", b.Key)
		}
	}
}

// reRenderMain runs on a ticker that starts before the first layout pass, so
// it must tolerate a missing view.
func TestReRenderMainWithoutView(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if err := gui.reRenderMain(); err != nil {
		t.Fatalf("reRenderMain: %v", err)
	}
}

func TestQuitReturnsErrQuit(t *testing.T) {
	gui := New()

	if err := gui.quit(nil, nil); err != gocui.ErrQuit {
		t.Errorf("quit() = %v, want gocui.ErrQuit", err)
	}
}
