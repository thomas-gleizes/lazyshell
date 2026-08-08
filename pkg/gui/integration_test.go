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
	skipMainLoopUnderRace(t)

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
			gui.setSelectedIndex(i)
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
		gui.setSelectedIndex(0)
		gui.onSelectionChanged()

		return nil
	})

	waitForOutput(t, g, markers[0])
}

// This is the roadmap's phase 4 exit criterion, end to end: focus the output
// panel, enter pass-through, drive a real shell, leave pass-through, confirm
// navigation (Tab) works again, and confirm the pty was actually resized to
// the panel's real geometry — the mechanics behind "dogfood 2-3 shells
// without having to kill the app". editOutput itself (key translation,
// prefix automaton, scroll clamping) is already covered directly in
// input_test.go; this test is about the wiring around it running through a
// real MainLoop.
func TestPassThroughDogfoodingFlow(t *testing.T) {
	skipMainLoopUnderRace(t)

	gui, g := newHeadlessGui(t)

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

	if _, err := gui.sessions.New("s0", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	outputView := waitForView(t, g, outputViewName)

	// Focus output and enter pass-through with "i".
	g.Update(func(*gocui.Gui) error {
		if _, err := g.SetCurrentView(outputViewName); err != nil {
			return err
		}

		gui.editOutput(outputView, 0, 'i', gocui.ModNone)

		return nil
	})

	waitForCondition(t, func() bool { return gui.passThroughActive }, "did not enter pass-through")

	// Type a command and see its result — the actual dogfooding gesture.
	g.Update(func(*gocui.Gui) error {
		for _, ch := range "echo dogfooding-ok" {
			gui.editOutput(outputView, 0, ch, gocui.ModNone)
		}

		gui.editOutput(outputView, gocui.KeyEnter, 0, gocui.ModNone)

		return nil
	})

	waitForOutput(t, g, "dogfooding-ok")

	// Leave pass-through: the prefix arms, any other key confirms the exit —
	// the same two-step automaton input_test.go exercises directly.
	g.Update(func(*gocui.Gui) error {
		gui.editOutput(outputView, gui.prefixKey, 0, gocui.ModNone)
		gui.editOutput(outputView, gocui.KeyEsc, 0, gocui.ModNone)

		return nil
	})

	waitForCondition(t, func() bool { return !gui.passThroughActive }, "the prefix did not leave pass-through")

	// Tab must work again now that we are not in pass-through — Tab itself
	// is swallowed by editOutput while pass-through is armed.
	g.Update(func(*gocui.Gui) error { return gui.cycleFocus(g, nil) })

	waitForCondition(t, func() bool {
		current := g.CurrentView()

		return current != nil && current.Name() == sessionsViewName
	}, "Tab after leaving pass-through did not return focus to sessions")

	// Resize propagation: stty size in the shell must reflect the output
	// panel's actual visible geometry, not the 80x24 pty default — and not
	// Size() either, which counts the two frame border rows/columns that are
	// not actually drawn (see propagateResize's comment in layout.go).
	cols, rows := outputView.InnerSize()

	sess := gui.selectedSession()
	if sess == nil {
		t.Fatal("no session selected")
	}

	if _, err := sess.Write([]byte("stty size\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForOutput(t, g, fmt.Sprintf("%d %d", rows, cols))
}
