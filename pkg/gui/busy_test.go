package gui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"
)

func TestSpinnerFrameCycles(t *testing.T) {
	first := spinnerFrame(0)

	if got := spinnerFrame(len(spinnerFrames)); got != first {
		t.Errorf("spinnerFrame(%d) = %q, want it back at %q", len(spinnerFrames), got, first)
	}

	// The counter only ever grows, so a long-running operation must not walk
	// off the end of the table.
	if got := spinnerFrame(10_000); got == "" {
		t.Error("spinnerFrame(10000) returned an empty frame")
	}
}

// Every frame must be one cell wide, or the popup's width would change under
// the animation — see spinnerFrames' doc comment.
func TestSpinnerFramesAreAllTheSameWidth(t *testing.T) {
	for _, frame := range spinnerFrames {
		if got := len([]rune(frame)); got != 1 {
			t.Errorf("frame %q is %d runes wide, want 1", frame, got)
		}
	}
}

func TestBusyContentShowsFrameThenMessage(t *testing.T) {
	got := busyContent("stopping session t...", 0)

	if !strings.HasPrefix(got, spinnerFrames[0]) {
		t.Errorf("busyContent() = %q, want it to start with the current frame", got)
	}
	if !strings.HasSuffix(got, "stopping session t...") {
		t.Errorf("busyContent() = %q, want it to end with the message", got)
	}
}

// Inline mode is what every test without a MainLoop relies on: the operation
// runs synchronously and its outcome is readable straight afterwards, with no
// popup left behind.
func TestRunBusyInlineReportsSynchronously(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	ran := false
	if err := gui.runBusy("working...", func() error {
		ran = true

		return nil
	}); err != nil {
		t.Fatalf("runBusy: %v", err)
	}

	if !ran {
		t.Error("inline runBusy did not run the operation")
	}
	if _, err := g.View(busyViewName); err == nil {
		t.Error("inline runBusy left a popup behind")
	}
}

func TestRunBusyInlineReportsFailureInTheStatusBar(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.runBusy("working...", func() error { return errors.New("boom") }); err != nil {
		t.Fatalf("runBusy: %v", err)
	}

	if gui.lastError != "boom" {
		t.Errorf("lastError = %q, want %q", gui.lastError, "boom")
	}
}

func TestRunBusyThenGivesItsTailTheOperationError(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	want := errors.New("boom")

	var got error
	if err := gui.runBusyThen("working...", func() error { return want }, func(err error) error {
		got = err

		return nil
	}); err != nil {
		t.Fatalf("runBusyThen: %v", err)
	}

	if !errors.Is(got, want) {
		t.Errorf("tail got %v, want %v", got, want)
	}
}

// The whole point of the popup: a slow operation must leave the interface
// drawing and the popup visible while it runs, then disappear on its own. End
// to end through a real MainLoop, since completion is delivered by a g.Update
// only MainLoop drains.
func TestRunBusyShowsPopupWhileWorkingThenTearsItDown(t *testing.T) {
	skipMainLoopUnderRace(t)

	gui, g := newHeadlessGui(t)
	// This test is exactly the case newHeadlessGui's inline default exists to
	// avoid — it has a MainLoop, so it wants the real asynchronous path.
	gui.busyInline = false

	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)

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

	waitForView(t, g, sessionsViewName)

	release := make(chan struct{})
	g.Update(func(*gocui.Gui) error {
		return gui.runBusy("working...", func() error {
			<-release

			return nil
		})
	})

	waitForCondition(t, func() bool {
		return onGuiGoroutine(g, func() bool {
			view, err := g.View(busyViewName)
			if err != nil {
				return false
			}

			return strings.Contains(view.Buffer(), "working...") &&
				g.CurrentView() != nil && g.CurrentView().Name() == busyViewName
		})
	}, "the busy popup never appeared with focus")

	// It animates: the frame counter has to move on its own while the
	// operation is still blocked.
	waitForCondition(t, func() bool {
		return onGuiGoroutine(g, func() bool { return gui.busyFrame > 0 })
	}, "the spinner never advanced a frame")

	close(release)

	waitForCondition(t, func() bool {
		return onGuiGoroutine(g, func() bool {
			_, err := g.View(busyViewName)

			return err != nil && !gui.busyActive
		})
	}, "the busy popup was never torn down")

	if !onGuiGoroutine(g, func() bool {
		return g.CurrentView() != nil && g.CurrentView().Name() == sessionsViewName
	}) {
		t.Error("focus did not go back to the view that had it before the popup")
	}
}

// A second operation cannot be started while one is up — the popup holds
// focus, but the guard is what makes that a rule rather than a side effect.
func TestRunBusyRefusesASecondOperationWhileOneIsUp(t *testing.T) {
	gui, g := newHeadlessGui(t)
	gui.busyInline = false

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	release := make(chan struct{})
	// Torn down by hand: with no MainLoop draining g.Update, the completion
	// this test deliberately never waits for would otherwise leave the
	// animation goroutine ticking for the rest of the run.
	t.Cleanup(func() {
		close(release)

		_ = gui.closeBusy()
	})

	if err := gui.runBusy("first", func() error {
		<-release

		return nil
	}); err != nil {
		t.Fatalf("runBusy: %v", err)
	}

	second := false
	if err := gui.runBusy("second", func() error {
		second = true

		return nil
	}); err != nil {
		t.Fatalf("runBusy: %v", err)
	}

	if second {
		t.Error("a second operation started while the first was still running")
	}
}
