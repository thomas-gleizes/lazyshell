package gui

import "github.com/jesseduffield/gocui"

// jumpToPreviousPrompt/jumpToNextPrompt are jumpToPrompt(-1)/jumpToPrompt(1)
// wrapped as keybinding handlers — see bindings() in keybindings.go, and
// editDuringScroll in input.go for the hand-matched path that reaches these
// while the output view is Editable.
func (gui *Gui) jumpToPreviousPrompt(*gocui.Gui, *gocui.View) error {
	gui.jumpToPrompt(-1)

	return nil
}

func (gui *Gui) jumpToNextPrompt(*gocui.Gui, *gocui.View) error {
	gui.jumpToPrompt(1)

	return nil
}

// jumpToPrompt scrolls the output panel to the nearest prompt boundary (per
// OSC 133 shell integration) before (delta < 0) or after (delta > 0) the line
// currently at the top of the window. Clamps at the oldest/newest mark rather
// than wrapping, unlike search's nextMatch: "the next prompt" past the newest
// one has no sensible target to land on. No-op with nothing selected, no
// marks recorded, or a full-screen application in control — same gate
// enterCopyMode uses, since there is nothing addressable to jump within
// while the alternate screen owns the session.
//
// Unlike search.go's jumpToMatch, there is no persistent index to maintain
// here and so nothing to invalidate on a selection change: PromptMarks
// always returns freshly-translated, currently-valid indices (see
// docs/adr/0008-integration-shell-osc-133.md), so this simply recomputes
// from the current scroll position on every call.
func (gui *Gui) jumpToPrompt(delta int) {
	sess := gui.selectedSession()
	if sess == nil || sess.Screen().IsAltScreen() {
		return
	}

	scr := sess.Screen()
	marks := scr.PromptMarks()
	if len(marks) == 0 {
		return
	}

	top := scr.ScrollbackLen() - gui.getScrollOffset()

	target, found := -1, false
	switch {
	case delta < 0:
		for i := len(marks) - 1; i >= 0; i-- {
			if marks[i] < top {
				target, found = marks[i], true

				break
			}
		}
	case delta > 0:
		for _, m := range marks {
			if m > top {
				target, found = m, true

				break
			}
		}
	}

	if !found {
		return
	}

	offset := scr.ScrollbackLen() - target
	if offset < 0 {
		offset = 0
	} else if max := scr.ScrollbackLen(); offset > max {
		offset = max
	}

	gui.setScrollOffset(offset)
	gui.restartOutput()
}
