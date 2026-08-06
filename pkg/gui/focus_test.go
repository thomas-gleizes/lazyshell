package gui

import (
	"testing"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
)

// newHeadlessGocui builds a bare gocui instance rendering to an in-memory
// screen, for tests that only need gocui's view/current-view bookkeeping, not
// the full Gui wrapper.
func newHeadlessGocui(t *testing.T) *gocui.Gui {
	t.Helper()

	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode: gocui.OutputTrue,
		Headless:   true,
		Width:      80,
		Height:     24,
	})
	if err != nil {
		t.Fatalf("NewGui: %v", err)
	}
	t.Cleanup(g.Close)

	return g
}

func setCurrentView(t *testing.T, g *gocui.Gui, name string) {
	t.Helper()

	if _, err := g.SetView(name, 0, 0, 10, 10, 0); err != nil && !goerrors.Is(err, gocui.ErrUnknownView) {
		t.Fatalf("SetView: %v", err)
	}

	if _, err := g.SetCurrentView(name); err != nil {
		t.Fatalf("SetCurrentView(%q): %v", name, err)
	}
}

func TestFocusManagerFiresLostThenGained(t *testing.T) {
	g := newHeadlessGocui(t)
	f := newFocusManager()

	var events []string
	f.onFocus["a"] = func() { events = append(events, "focus:a") }
	f.onFocusLost["a"] = func() { events = append(events, "lost:a") }
	f.onFocus["b"] = func() { events = append(events, "focus:b") }
	f.onFocusLost["b"] = func() { events = append(events, "lost:b") }

	setCurrentView(t, g, "a")
	if err := f.Layout(g); err != nil {
		t.Fatalf("Layout: %v", err)
	}

	setCurrentView(t, g, "b")
	if err := f.Layout(g); err != nil {
		t.Fatalf("Layout: %v", err)
	}

	want := []string{"focus:a", "lost:a", "focus:b"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("events[%d] = %q, want %q (full: %v)", i, events[i], want[i], events)
		}
	}
}

func TestFocusManagerNoChangeFiresNothing(t *testing.T) {
	g := newHeadlessGocui(t)
	f := newFocusManager()

	calls := 0
	f.onFocus["a"] = func() { calls++ }

	setCurrentView(t, g, "a")

	if err := f.Layout(g); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if err := f.Layout(g); err != nil {
		t.Fatalf("Layout: %v", err)
	}

	if calls != 1 {
		t.Errorf("onFocus fired %d times across two unchanged Layout calls, want 1", calls)
	}
}
