package gui

import (
	"fmt"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
)

const confirmViewName = "confirm"

// showConfirm opens a small centered popup with message and waits for y/n/Esc.
// It is not part of the boxlayout tree: it is sized directly from the
// terminal's dimensions and torn down again on dismissal, the same way
// lazygit's popups work.
func (gui *Gui) showConfirm(message string, onConfirm func() error) error {
	maxX, maxY := gui.g.Size()

	width := min(len(message)+4, maxX-2)
	if width < 4 {
		width = 4
	}
	height := 2

	x0 := (maxX - width) / 2
	y0 := (maxY - height) / 2

	view, err := gui.g.SetView(confirmViewName, x0, y0, x0+width, y0+height, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = gui.tr.T("confirm.title")
	}

	view.Clear()
	fmt.Fprint(view, message)

	if _, err := gui.g.SetCurrentView(confirmViewName); err != nil {
		return err
	}

	if err := gui.g.SetKeybinding(confirmViewName, 'y', gocui.ModNone, func(*gocui.Gui, *gocui.View) error {
		return gui.acceptConfirm(onConfirm)
	}); err != nil {
		return err
	}

	for _, key := range []any{'n', gocui.KeyEsc} {
		if err := gui.g.SetKeybinding(confirmViewName, key, gocui.ModNone, func(*gocui.Gui, *gocui.View) error {
			return gui.closeConfirm()
		}); err != nil {
			return err
		}
	}

	return nil
}

// acceptConfirm is the 'y' keybinding's handler, factored out so it can be
// exercised directly in tests without going through gocui's key dispatch
// (which the headless test Gui has no way to simulate).
func (gui *Gui) acceptConfirm(onConfirm func() error) error {
	// Torn down *before* the callback runs, same order as submitPrompt's:
	// an onConfirm that opens another popup of its own — every one of them
	// currently does, via runBusy — would otherwise have its focus taken
	// straight back by closeConfirm's SetCurrentView.
	if err := gui.closeConfirm(); err != nil {
		return err
	}

	if err := onConfirm(); err != nil {
		// gocui.ErrQuit is not a failure to report — it is the sentinel
		// MainLoop watches for (see gui.go's Run). quit's onConfirm returns
		// it, and it must reach MainLoop unchanged; swallowing it into
		// lastError like every other error would turn "confirm quit" into a
		// dead "Ex: quit" message and the app would never actually exit.
		if goerrors.Is(err, gocui.ErrQuit) {
			return err
		}

		gui.lastError = err.Error()
	} else {
		gui.lastError = ""
		gui.lastInfo = ""
	}

	// closeConfirm already redrew both, but that was before the callback ran
	// and set the message it may want shown.
	return gui.refreshAfterBusy()
}

// closeConfirm tears the popup down and restores focus to the sessions panel.
func (gui *Gui) closeConfirm() error {
	gui.g.DeleteViewKeybindings(confirmViewName)

	if err := gui.g.DeleteView(confirmViewName); err != nil {
		return err
	}

	if _, err := gui.g.SetCurrentView(sessionsViewName); err != nil {
		return err
	}

	if view, err := gui.g.View(statusViewName); err == nil {
		gui.renderStatus(view)
	}

	return gui.renderSessionsPanel()
}
