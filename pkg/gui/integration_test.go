package gui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"
)

// waitForOutput polls the output view's buffer until it contains want. gui's
// output rendering goes through g.Update, which is only ever drained by a
// running MainLoop — this test is the one place that runs a real one.
func waitForOutput(t *testing.T, g *gocui.Gui, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		view, err := g.View(outputViewName)
		if err == nil && strings.Contains(view.Buffer(), want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	view, _ := g.View(outputViewName)
	buf := ""
	if view != nil {
		buf = view.Buffer()
	}

	t.Fatalf("timed out waiting for %q in the output view:\n%s", want, buf)
}

// This is the roadmap's phase 3 exit criterion, end to end: create 3
// sessions, alternate the selection, see each one's output — and a session
// that produced output while it was not selected must show it immediately on
// return, with nothing lost. That last part is guaranteed by pkg/session's
// drain goroutines running independently of what is on screen (phase 2); this
// test is what proves the gui layer built on top of it does not break that.
func TestSelectingSessionsShowsEachOnesOutputWithoutLoss(t *testing.T) {
	gui, g := newHeadlessGui(t)

	// SetManager purges existing keybindings, so it must run first — same
	// order as Gui.Run.
	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)

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

	const n = 3

	markers := make([]string, n)
	for i := range n {
		marker := fmt.Sprintf("marker-%d", i)
		markers[i] = marker

		sess, err := gui.sessions.New(fmt.Sprintf("s%d", i), "/bin/sh")
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		if _, err := sess.Write([]byte("echo " + marker + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	// Select each session in turn and confirm its own marker shows up.
	for i, marker := range markers {
		g.Update(func(*gocui.Gui) error {
			gui.selectedIndex = i
			gui.onSelectionChanged()

			return nil
		})

		waitForOutput(t, g, marker)
	}

	// By now sessions 0 and 1 produced their output while not selected (2 and
	// then nothing was displaying them). Selecting session 0 again must show
	// its marker right away — proof that nothing was lost while it was
	// hidden, not that it gets re-executed.
	g.Update(func(*gocui.Gui) error {
		gui.selectedIndex = 0
		gui.onSelectionChanged()

		return nil
	})

	waitForOutput(t, g, markers[0])
}
