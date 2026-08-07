package gui

import (
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// onGuiGoroutine evaluates cond on gocui's own goroutine and hands the answer
// back. gui's mode fields (passThroughActive, zoomed) and the current view are
// only ever touched from there, so a test with a MainLoop running cannot read
// them directly without racing the very handlers it is waiting on.
func onGuiGoroutine(g *gocui.Gui, cond func() bool) bool {
	result := make(chan bool, 1)

	g.Update(func(*gocui.Gui) error {
		result <- cond()

		return nil
	})

	return <-result
}

// Typing `exit` in a session while pass-through is armed must not leave the
// interface pointing at a dead shell: watchSelectedExit disarms pass-through
// and hands focus back to the sessions panel, on the same (now exited)
// session. End to end through a real MainLoop, because the whole mechanism
// hangs off a g.Update only MainLoop drains.
func TestExitFromInsideLeavesPassThroughAndRefocusesSessions(t *testing.T) {
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

	sess, err := gui.sessions.New("s0", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	outputView := waitForView(t, g, outputViewName)

	g.Update(func(*gocui.Gui) error {
		if _, err := g.SetCurrentView(outputViewName); err != nil {
			return err
		}

		gui.editOutput(outputView, 0, 'i', gocui.ModNone)

		return nil
	})

	waitForCondition(t, func() bool {
		return onGuiGoroutine(g, func() bool { return gui.passThroughActive })
	}, "did not enter pass-through")

	// The gesture under test: the shell is ended from the inside, not killed
	// from the panel.
	g.Update(func(*gocui.Gui) error {
		for _, ch := range "exit" {
			gui.editOutput(outputView, 0, ch, gocui.ModNone)
		}

		gui.editOutput(outputView, gocui.KeyEnter, 0, gocui.ModNone)

		return nil
	})

	waitForCondition(t, func() bool { return sess.Status() == session.StatusExited }, "the shell did not exit")

	// Nothing drives watchSelectedExit here — Run's ticker is not running in
	// this test — so it is called directly, as that ticker would.
	waitForCondition(t, func() bool {
		_ = gui.watchSelectedExit()

		return onGuiGoroutine(g, func() bool {
			current := g.CurrentView()

			return !gui.passThroughActive && current != nil && current.Name() == sessionsViewName
		})
	}, "the exited session did not take focus back to the sessions panel")

	// The session stays selected and listed: that is what makes "R" (restart)
	// and "x"/"D" reachable right after.
	if got := gui.selectedSession(); got == nil || got.ID != sess.ID {
		t.Errorf("selected session after exit = %v, want %s", got, sess.ID)
	}
}

// watchSelectedExit must fire once per exit, not on every tick: the exited
// session stays selected afterwards, and re-firing would pin focus to the
// sessions panel and make Tab useless.
// Only the bookkeeping is exercised here — no pty needed to decide whether an
// exit is news.
func TestWatchSelectedExitFiresOncePerExit(t *testing.T) {
	gui := &Gui{}

	if !gui.markExitHandled("s0") {
		t.Fatal("first exit was not treated as news")
	}

	if gui.markExitHandled("s0") {
		t.Error("the same exit was reacted to twice")
	}

	// Selecting elsewhere must not re-arm it — coming back would otherwise
	// bounce the focus a second time.
	gui.clearExitHandled("other-session")

	if gui.markExitHandled("s0") {
		t.Error("another session's selection re-armed the exited one")
	}

	// A restart reuses the same id (Manager.Restart), and its own later exit
	// must be caught: watchSelectedExit clears the mark as soon as it sees the
	// session running again.
	gui.clearExitHandled("s0")

	if !gui.markExitHandled("s0") {
		t.Error("a restarted session's exit was swallowed")
	}
}

// With a popup open (confirm, help, prompt), the current view is not the
// output panel and focus must be left alone — the user is in the middle of
// something else. backOutOfExitedSession is called directly here: it is what
// watchSelectedExit hands to g.Update, and no MainLoop runs in this test to
// drain that queue.
func TestBackOutOfExitedSessionLeavesPopupFocusAlone(t *testing.T) {
	gui, g := newHeadlessGui(t)

	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)

	// No need for the session to actually be dead: watchSelectedExit is what
	// decides *when* to back out, backOutOfExitedSession only carries it out.
	if _, err := gui.sessions.New("s0", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.showConfirm("kill? (y/n)", func() error { return nil }); err != nil {
		t.Fatalf("showConfirm: %v", err)
	}

	gui.passThroughActive = true

	if err := gui.backOutOfExitedSession(g); err != nil {
		t.Fatalf("backOutOfExitedSession: %v", err)
	}

	if !gui.passThroughActive {
		t.Error("pass-through was disarmed while a popup had focus")
	}

	if current := g.CurrentView(); current == nil || current.Name() != confirmViewName {
		t.Errorf("current view = %v, want the confirm popup to keep focus", current)
	}
}

// Zoomed, the sessions panel does not exist to receive focus: pass-through is
// still disarmed, which is what stops keystrokes from going to a dead shell.
func TestBackOutOfExitedSessionWhileZoomed(t *testing.T) {
	gui, g := newHeadlessGui(t)

	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)

	if _, err := gui.sessions.New("s0", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	// Zoom the way the user does — from a laid-out, focused interface: layout
	// leaves the sessions panel out entirely while zoomed, so it has to exist
	// (and not be the current view) before that happens.
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.toggleZoom(g, nil); err != nil {
		t.Fatalf("toggleZoom: %v", err)
	}

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.passThroughActive = true

	if err := gui.backOutOfExitedSession(g); err != nil {
		t.Fatalf("backOutOfExitedSession: %v", err)
	}

	if gui.passThroughActive {
		t.Error("pass-through stayed armed on a dead session while zoomed")
	}

	if current := g.CurrentView(); current == nil || current.Name() != outputViewName {
		t.Errorf("current view = %v, want the output panel (no sessions panel while zoomed)", current)
	}
}
