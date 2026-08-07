package gui

import (
	"context"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// outputFrame is everything a single render tick can change about the output
// panel. Comparing two of them is how showOutput decides whether it has
// anything to push at all — see its own comment.
type outputFrame struct {
	content string

	// cursorShown, cursorX and cursorY describe the real terminal cursor as
	// drawn for this frame. They belong here rather than being derived at draw
	// time because the cursor moves without the screen content changing (typing
	// at a prompt that has not echoed yet, vim moving through a static buffer),
	// and skipping that update would leave the cursor visibly stuck.
	cursorShown bool
	cursorX     int
	cursorY     int
}

// showOutput starts a task that renders sess's screen into the output view on
// every tick, until superseded by another call (a different session gets
// selected, the scroll offset changes, the mode or the focus changes) or the
// application quits. The task only owns this rendering loop: cancelling it
// never touches sess's process or its drain goroutine — those belong to
// pkg/session and keep running regardless of what is currently displayed,
// which is what guarantees no output is ever lost while a session is hidden.
//
// The scroll offset and the pass-through flag are captured once, here, rather
// than re-read on every tick: both are written from gocui's main goroutine
// (keybinding handlers, the output Editor) while this closure runs on its own,
// so reading them repeatedly from here would race. Every place that changes
// either one already calls back into showOutput to restart the task with
// freshly captured values, the same pattern the TaskManager is built for.
//
// An unchanged frame is not pushed at all. Without that check the panel called
// g.Update on every tick whether or not anything had happened, and a g.Update
// redraws the *whole* screen: an idle lazyshell repainted every panel ~33
// times a second forever. The comparison state lives in the closure rather
// than on Gui because the TaskManager runs this function serially in a single
// goroutine, so it needs no guarding — and a new task starts with nothing
// drawn, which is what makes the first tick after any showOutput call always
// draw.
func (gui *Gui) showOutput(sess *session.Session) {
	// Captured with the rest, for the same reason: the active tab is written
	// from gocui's goroutine, and every place that changes it already calls
	// back into here.
	tab := gui.outputTab
	// Resolved on this goroutine too — gui.tr is immutable after construction,
	// but keeping every string the task needs in its closure is what makes
	// "the task touches no Gui state" true rather than nearly true.
	placeholder := gui.tr.T("tab.placeholder")

	offset := gui.getScrollOffset()
	passThrough := gui.passThroughActive
	pattern := gui.searchPattern

	selFrom, selTo := -1, -1
	if gui.copyModeActive {
		selFrom, selTo = gui.copySelectionRange()
	}

	var (
		previous outputFrame
		drawn    bool
	)

	gui.outputTasks.NewTickerTask(gui.tick(), func(context.Context) {
		// One builder per tab, all producing the same comparable outputFrame,
		// so the skip-if-unchanged rule below covers the new tabs too. That
		// matters most for them: a report that only changes once a second
		// would otherwise repaint the whole screen on every tick.
		frame := outputFrame{content: placeholder}
		if tab == tabOutput {
			frame = buildOutputFrame(sess, offset, passThrough, pattern, selFrom, selTo)
		}

		if drawn && frame == previous {
			return
		}

		previous, drawn = frame, true

		gui.g.Update(func(g *gocui.Gui) error {
			view, err := g.View(outputViewName)
			if err != nil {
				// The view is gone (not laid out yet, or a resize dropped it):
				// forget this frame so the next tick draws again rather than
				// comparing against something never displayed.
				drawn = false

				return nil
			}

			view.SetContent(frame.content)
			drawCursor(g, view, frame)

			return nil
		})
	})
}

// buildOutputFrame reads everything this tick needs from the session's
// emulator. Runs on the task's own goroutine, so it touches no Gui state:
// offset, passThrough, pattern and the selFrom/selTo copy-mode range were
// captured by showOutput on gocui's. selFrom < 0 means "no selection" — the
// pattern is what the panel highlights instead in that case, the same
// tradeoff RenderAt already makes; the two never both apply, since entering
// copy-mode and starting a search are mutually exclusive (see
// editDuringScroll).
func buildOutputFrame(sess *session.Session, offset int, passThrough bool, pattern string, selFrom, selTo int) outputFrame {
	scr := sess.Screen()

	var content string
	if selFrom >= 0 {
		content = scr.RenderAtSelection(offset, selFrom, selTo)
	} else {
		content = scr.RenderAt(offset, pattern)
	}

	frame := outputFrame{content: content}

	// The cursor is only meaningful on the live screen: scrolled back, the
	// user is looking at history the cursor is not in. And it is only drawn
	// while the session is actually being typed into, so it never sits blinking
	// on a panel the keyboard is not connected to.
	if offset == 0 && passThrough && scr.CursorVisible() {
		frame.cursorShown = true
		frame.cursorX, frame.cursorY = scr.CursorPosition()
	}

	return frame
}

// drawCursor places gocui's real terminal cursor where the emulator's is —
// without it, vim and htop are unusable: you cannot see where you are typing.
//
// Must run inside a g.Update: SetCursor and g.Cursor are plain field writes
// with none of View.Write's internal locking, so touching them from the task's
// own goroutine would race gocui's render pass — the same reasoning as
// renderSessionsPanel's.
func drawCursor(g *gocui.Gui, view *gocui.View, frame outputFrame) {
	current := g.CurrentView()
	if !frame.cursorShown || current == nil || current.Name() != outputViewName {
		g.Cursor = false

		return
	}

	g.Cursor = true
	// The view never scrolls (origin stays 0,0 — the emulator decides what is
	// on screen), so emulator coordinates are view coordinates.
	view.SetCursor(frame.cursorX, frame.cursorY)
}

// restartOutput restarts the render task for the selected session, so that
// state captured at task-start time (scroll offset, pass-through) is picked up
// and the next tick redraws unconditionally. Called by everything that changes
// how the panel should look without changing what the session has produced.
func (gui *Gui) restartOutput() {
	if sess := gui.selectedSession(); sess != nil {
		gui.showOutput(sess)
	}
}
