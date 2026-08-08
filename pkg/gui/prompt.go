package gui

import (
	"fmt"
	"strings"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
)

const promptViewName = "prompt"

// promptMinWidth/promptHeight size the single-line text-entry popup: wide
// enough for a short answer to be comfortable, one line of input plus the
// frame gocui always draws around a titled view.
//
// Both are gocui *corner-to-corner* distances, not inner sizes: SetView(x0,
// y0, x1, y1) spans y1-y0+1 rows of which the frame eats two, so height must
// be 2 to leave a single usable line. With 1 the popup renders as a bare
// frame with nowhere to put the text or the cursor.
const (
	promptMinWidth = 40
	promptHeight   = 2
)

// showPrompt opens a single-line text-entry popup titled title, pre-filled
// with initial (cursor placed at its end), and calls onSubmit with the
// trimmed input on Enter. Esc cancels without calling onSubmit. Same
// popup-view shape as confirm.go's showConfirm, but editable: gocui.
// DefaultEditor drives ordinary typing, Enter/Esc are handled here as
// view-scoped keybindings, which always win over the Editor (see initView's
// note on outputViewName), so they never insert a literal newline/escape
// byte into the buffer.
func (gui *Gui) showPrompt(title, initial string, onSubmit func(string) error) error {
	maxX, maxY := gui.g.Size()

	width := min(max(promptMinWidth, len(initial)+10), maxX-2)
	height := min(promptHeight, maxY-2)

	x0 := (maxX - width) / 2
	y0 := (maxY - height) / 2

	view, err := gui.g.SetView(promptViewName, x0, y0, x0+width, y0+height, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = " " + title + " "
		view.Editable = true
		view.Editor = gocui.DefaultEditor
		view.Wrap = false
	}

	view.Clear()
	fmt.Fprint(view, initial)
	view.SetCursor(len(initial), 0)

	if current := gui.g.CurrentView(); current != nil {
		gui.promptReturnView = current.Name()
	}

	if _, err := gui.g.SetCurrentView(promptViewName); err != nil {
		return err
	}

	gui.promptOnSubmit = onSubmit

	if err := gui.g.SetKeybinding(promptViewName, gocui.KeyEnter, gocui.ModNone, gui.submitPrompt); err != nil {
		return err
	}

	return gui.g.SetKeybinding(promptViewName, gocui.KeyEsc, gocui.ModNone, gui.cancelPrompt)
}

// cancelPrompt is Esc's handler: it tears the popup down without ever
// calling the captured onSubmit callback.
func (gui *Gui) cancelPrompt(*gocui.Gui, *gocui.View) error {
	gui.promptOnSubmit = nil

	return gui.closePrompt()
}

// submitPrompt reads the popup's buffer, tears it down, then runs the
// callback captured by showPrompt — in that order, so a callback that opens
// another popup (or re-renders the sessions panel) is not fighting the one
// being closed.
func (gui *Gui) submitPrompt(g *gocui.Gui, v *gocui.View) error {
	text := strings.TrimSpace(v.Buffer())
	onSubmit := gui.promptOnSubmit
	gui.promptOnSubmit = nil

	if err := gui.closePrompt(); err != nil {
		return err
	}

	if onSubmit == nil {
		return nil
	}

	// Snapshotted so the generic tail below cannot undo a callback that
	// reported its own outcome: onExportSubmit calls reportSessionError/
	// reportSessionInfo and returns nil, and clearing lastError on that nil
	// would wipe the very message it had just put there.
	beforeError, beforeInfo := gui.lastError, gui.lastInfo

	switch err := onSubmit(text); {
	case err != nil:
		gui.lastError = err.Error()
	case gui.lastError == beforeError && gui.lastInfo == beforeInfo:
		// The callback said nothing of its own: the submission succeeded, so
		// whatever an earlier action had left in the status bar goes.
		gui.lastError = ""
	}

	if view, err := g.View(statusViewName); err == nil {
		gui.renderStatus(view)
	}

	return gui.renderSessionsPanel()
}

// closePrompt tears the popup down and restores focus to whichever view was
// current before showPrompt opened it.
func (gui *Gui) closePrompt() error {
	gui.g.DeleteViewKeybindings(promptViewName)

	if err := gui.g.DeleteView(promptViewName); err != nil {
		return err
	}

	returnView := gui.promptReturnView
	if returnView == "" {
		returnView = sessionsViewName
	}

	_, err := gui.g.SetCurrentView(returnView)

	return err
}
