package gui

import (
	"fmt"
	"time"
	"unicode/utf8"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
)

const busyViewName = "busy"

// spinnerFrames is the animation itself: braille dots, the same set lazygit
// and lazydocker use, so a terminal that renders their spinners renders this
// one too. Ten frames of a single cell each — the popup's width never changes
// mid-animation, which it would with frames of differing widths.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerInterval is how fast the frames advance. Deliberately slower than
// reRenderInterval (30ms): a spinner stepping on every redraw tick reads as
// noise rather than as motion, and each step costs a full-screen g.Update.
const spinnerInterval = 100 * time.Millisecond

// busyMinWidth keeps a very short message from producing a popup too narrow
// to read as one, the same way promptMinWidth does for the input popup.
const busyMinWidth = 24

// spinnerFrame returns the frame at index i, cycling. Takes any int (the
// counter only ever grows) so callers never have to do the modulo themselves.
func spinnerFrame(i int) string {
	if i < 0 {
		i = -i
	}

	return spinnerFrames[i%len(spinnerFrames)]
}

// busyContent is what the popup shows: the current frame, then the message.
// Pure, so the animation can be tested without a gocui screen.
func busyContent(message string, frame int) string {
	return spinnerFrame(frame) + " " + message
}

// runBusy runs op on a background goroutine behind a small centered popup
// with an animated spinner, and reports its outcome the way the rest of the
// interface does (status bar, sessions panel) once it returns.
//
// It exists because the operations it wraps are *slow enough to be noticed*:
// Session.Kill can wait up to twice KillTimeout (4s by default) for a shell
// to be reaped, and an external clipboard command can take as long as it
// likes. Running those inline on gocui's own goroutine froze the whole
// interface — no redraw, no keystroke handled — with nothing on screen to say
// why, which reads as a hang rather than as work in progress.
//
// The popup is not part of the boxlayout tree, same as confirm.go's: created
// on the spot, torn down on completion. It takes focus while it is up, which
// is also what makes it modal — every keybinding in this codebase is
// view-scoped, so with the busy view current none of them can fire and the
// user cannot start a second operation on top of the first.
func (gui *Gui) runBusy(message string, op func() error) error {
	return gui.runBusyThen(message, op, nil)
}

// runBusyThen is runBusy with a caller-supplied tail: onDone runs on gocui's
// goroutine once op has returned and the popup is gone, and takes over
// reporting entirely (op's error is handed to it rather than pushed to the
// status bar). For operations whose success is worth saying out loud — the
// export's "written to ..." — which op itself cannot report, since it runs on
// a background goroutine where touching gui state or a view is not allowed.
func (gui *Gui) runBusyThen(message string, op func() error, onDone func(error) error) error {
	// No main loop, no spinner: the completion below is delivered through
	// g.Update, which only *enqueues* work — with nothing pumping that queue
	// the operation would never be reported, and with no gocui instance at all
	// there is not even a screen to draw the popup on. Both cases run the work
	// inline instead, which is exactly the behaviour these call sites had
	// before the spinner existed. See Gui.busyInline.
	if gui.g == nil || gui.busyInline {
		return gui.finishBusy(op(), onDone)
	}

	// Guard rather than queue: the only way to reach a second runBusy while
	// one is up would be a background goroutine, and none of them start one.
	// Same concurrency rule as passThroughActive — busy state is only ever
	// touched from gocui's own goroutine, either directly from a keybinding
	// handler or from inside a g.Update closure.
	if gui.busyActive {
		return nil
	}

	if current := gui.g.CurrentView(); current != nil {
		gui.busyReturnView = current.Name()
	}

	gui.busyActive = true
	gui.busyMessage = message
	gui.busyFrame = 0

	if err := gui.renderBusy(); err != nil {
		gui.busyActive = false

		return err
	}

	if _, err := gui.g.SetCurrentView(busyViewName); err != nil {
		gui.busyActive = false

		return err
	}

	gui.debug.Event("busy: %s", message)

	stop := make(chan struct{})
	gui.busyStop = stop

	go gui.animateBusy(stop)

	go func() {
		err := op()

		gui.g.Update(func(*gocui.Gui) error {
			return gui.finishBusy(err, onDone)
		})
	}()

	return nil
}

// animateBusy advances the spinner until stop is closed. The frame counter is
// bumped inside the g.Update closure, not here: it is read by renderBusy on
// gocui's goroutine, and this is the only goroutine that would otherwise
// write it from outside.
func (gui *Gui) animateBusy(stop chan struct{}) {
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			gui.g.Update(func(*gocui.Gui) error {
				if !gui.busyActive {
					return nil
				}

				gui.busyFrame++

				return gui.renderBusy()
			})
		}
	}
}

// renderBusy (re)draws the popup, sized from the message and centered. Called
// once at open time and again on every spinner tick — SetView on an existing
// view just returns it, so re-sizing it costs nothing and keeps the popup
// centered across a resize.
func (gui *Gui) renderBusy() error {
	maxX, maxY := gui.g.Size()

	content := busyContent(gui.busyMessage, gui.busyFrame)

	width := max(utf8.RuneCountInString(content)+4, busyMinWidth)
	if width > maxX-2 {
		width = maxX - 2
	}

	if width < 4 {
		width = 4
	}

	// Corner-to-corner, like promptHeight: two rows of frame plus the one
	// line of content.
	height := 2

	x0 := (maxX - width) / 2
	y0 := (maxY - height) / 2

	view, err := gui.g.SetView(busyViewName, x0, y0, x0+width, y0+height, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = gui.tr.T("busy.title")
	}

	view.Clear()
	fmt.Fprint(view, content)

	return nil
}

// finishBusy tears the popup down and reports the outcome. Runs on gocui's
// goroutine (either from the g.Update runBusyThen scheduled, or directly when
// there is no screen at all), so it is free to touch gui state and views.
func (gui *Gui) finishBusy(err error, onDone func(error) error) error {
	if err := gui.closeBusy(); err != nil {
		return err
	}

	if onDone != nil {
		return onDone(err)
	}

	if err != nil {
		gui.lastError = err.Error()
	} else {
		gui.lastError = ""
		gui.lastInfo = ""
	}

	return gui.refreshAfterBusy()
}

// closeBusy stops the animation, removes the view and gives focus back to
// whichever view had it when the popup opened. A no-op when no popup is up,
// which is the case runBusyThen's synchronous fallback goes through.
func (gui *Gui) closeBusy() error {
	if !gui.busyActive {
		return nil
	}

	gui.busyActive = false

	if gui.busyStop != nil {
		close(gui.busyStop)
		gui.busyStop = nil
	}

	if err := gui.g.DeleteView(busyViewName); err != nil && !goerrors.Is(err, gocui.ErrUnknownView) {
		return err
	}

	returnView := gui.busyReturnView
	if returnView == "" {
		returnView = sessionsViewName
	}

	// The view that had focus can be gone by now — the sessions panel is not
	// drawn while zoomed, and a busy operation is exactly the kind of thing
	// that outlives it. Falling back rather than failing, since there is no
	// user-facing error to report here: the work already happened.
	if _, err := gui.g.SetCurrentView(returnView); err != nil {
		if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
			return err
		}
	}

	return nil
}

// refreshAfterBusy redraws the two pieces of chrome an operation behind the
// spinner can have changed: the status bar (its message) and the sessions
// list (a session that just died or disappeared).
func (gui *Gui) refreshAfterBusy() error {
	if gui.g == nil {
		return nil
	}

	if view, err := gui.g.View(statusViewName); err == nil {
		gui.renderStatus(view)
	}

	return gui.renderSessionsPanel()
}
