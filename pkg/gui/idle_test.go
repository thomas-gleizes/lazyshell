package gui

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"
)

// countingManager is a third gocui.Manager, run after the layout and focus
// ones, that counts how many times gocui laid the screen out. A layout pass is
// exactly what a g.Update costs — gocui has no partial redraw — so this counts
// full-screen repaints.
type countingManager struct {
	passes atomic.Int64
}

func (c *countingManager) Layout(*gocui.Gui) error {
	c.passes.Add(1)

	return nil
}

// An idle lazyshell must not repaint. Both panels are driven by 30 ms tickers,
// and before phase 6 each one called g.Update on every single tick whether or
// not anything had changed — around 66 full-screen repaints a second, forever,
// with nothing on screen moving. The ADR 0001 measurement that a repaint is
// cheap only holds because the emulator bounds the view's contents; doing it 66
// times a second for nothing is still a laptop fan spinning up.
//
// This is a behaviour test, not a benchmark: it asserts the tickers stay quiet,
// which is the property the skip-if-unchanged logic exists to provide.
func TestIdleSessionDoesNotRepaint(t *testing.T) {
	skipMainLoopUnderRace(t)

	gui, g := newHeadlessGui(t)

	counter := &countingManager{}
	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus, counter)

	if err := gui.setKeybindings(g); err != nil {
		t.Fatalf("setKeybindings: %v", err)
	}

	loopDone := make(chan error, 1)
	go func() { loopDone <- g.MainLoop() }()
	t.Cleanup(func() {
		g.Update(func(*gocui.Gui) error { return gocui.ErrQuit })

		select {
		case <-loopDone:
		case <-time.After(2 * time.Second):
			t.Error("MainLoop did not stop")
		}
	})

	newTestSession(t, gui, "idle")

	g.Update(func(*gocui.Gui) error {
		gui.setSelectedIndex(0)
		gui.onSelectionChanged()

		return nil
	})

	gui.goEvery(reRenderInterval, gui.renderSessionsPanel)

	// Let the shell settle: it prints a prompt, answers the emulator's
	// capability queries, and those legitimately do repaint.
	time.Sleep(500 * time.Millisecond)

	before := counter.passes.Load()
	const window = time.Second
	time.Sleep(window)
	repaints := counter.passes.Load() - before

	// Both tickers running unconditionally would force ~66 repaints per second
	// (2 panels x 1s / 30ms). Anything in that region means the skip is not
	// working; a handful is the shell still stirring.
	const budget = 15

	if repaints > budget {
		t.Errorf("%d repaints in %v while idle, want at most %d", repaints, window, budget)
	}

	t.Logf("idle repaints: %d in %v", repaints, window)
}
