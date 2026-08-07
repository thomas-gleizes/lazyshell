package gui

import "github.com/jesseduffield/gocui"

// The output panel is a tab host: three ways of looking at the *same* selected
// session, not three panels. `output` is its emulated screen, `perf` what its
// process is consuming, `env` what it was started with — questions that were
// previously only answerable by leaving lazyshell for an `htop` or an
// `env | grep`.
//
// gocui already knows how to draw this: View.Tabs/TabIndex render the frame's
// top line as a tab strip and highlight the active entry (gui.go's drawTitle),
// and SetTabClickBinding routes a click on that line back here. So there is one
// gocui view, named `output`, whose *content* changes — not one view per tab.
// That distinction is load-bearing: propagateResize (layout.go) sizes every
// session's pty from this view's InnerSize, and a tab that made the view
// disappear from the layout would resize every shell on every tab switch.
type outputTab int

const (
	// tabOutput is the emulated screen — the default, and the only tab the
	// keyboard can be handed to a shell from.
	tabOutput outputTab = iota
	// tabPerf is the selected session's resource usage.
	tabPerf
	// tabEnv is the environment its shell was launched with.
	tabEnv

	// outputTabCount bounds switchTab's wrap-around. Kept next to the values
	// it counts so adding a fourth tab is one edit, not two.
	outputTabCount = 3
)

// tabLabels is the strip gocui draws, in tab order.
//
// Each label is padded with a space on both sides. That is not cosmetic:
// gocui's drawTitle starts painting at x0+2 while GetClickedTabIndex starts
// counting at x0+1 (view.go), so a click lands one column left of where the
// label was drawn. The padding absorbs that off-by-one — without it, clicking
// the first character of a tab selects the one before it.
func (gui *Gui) tabLabels() []string {
	return []string{
		" " + gui.tr.T("tab.output") + " ",
		" " + gui.tr.T("tab.perf") + " ",
		" " + gui.tr.T("tab.env") + " ",
	}
}

// switchTab moves delta tabs along, wrapping in both directions.
func (gui *Gui) switchTab(delta int) {
	next := (int(gui.outputTab) + delta) % outputTabCount
	if next < 0 {
		next += outputTabCount
	}

	gui.setTab(outputTab(next))
}

// setTab is the single place the active tab changes, so everything that must
// be true of a tab switch is stated once.
//
// Leaving the output tab disarms pass-through and copy-mode rather than
// merely hiding them: both are states *about the emulated screen*, and keeping
// them armed behind a panel that is no longer showing it would mean keystrokes
// still reaching a shell the user can no longer see. The scroll offset is
// deliberately *not* reset — coming back to the output tab lands exactly where
// the scrollback was left.
//
// Only ever called from gocui's own goroutine (a keybinding handler, the
// output Editor, a tab click), the same rule as passThroughActive and zoomed.
func (gui *Gui) setTab(tab outputTab) {
	if tab == gui.outputTab {
		return
	}

	gui.outputTab = tab
	gui.tabOffset = 0

	if gui.passThroughActive {
		// Also restarts the render task and refreshes the chrome, which is
		// why nothing below duplicates that.
		gui.exitPassThrough()
	}

	if gui.copyModeActive {
		gui.cancelCopyMode()
	}

	// Nil while a bare Gui{} literal is under test; the field above is still
	// the state of record, and layout re-reads it on the next initView.
	if gui.g != nil {
		if view, err := gui.g.View(outputViewName); err == nil {
			view.TabIndex = int(tab)
		}
	}

	gui.restartOutput()
	gui.refreshChrome()
}

// nextTab/prevTab are the keybinding handlers. They are registered on the
// sessions view rather than the output one on purpose — see the comment on
// their entries in staticBindings.
func (gui *Gui) nextTab(*gocui.Gui, *gocui.View) error {
	gui.switchTab(1)

	return nil
}

func (gui *Gui) prevTab(*gocui.Gui, *gocui.View) error {
	gui.switchTab(-1)

	return nil
}

// clickOutputTab is what gocui calls when the frame's tab strip is clicked; it
// hands over the index it resolved, never coordinates.
//
// Known limitation: gocui consults ShouldHandleMouseEvent before it ever gets
// to the tab strip (gui.go), and shouldHandleMouseEvent refuses everything on
// this view while pass-through is armed and the session's program has not
// asked for the mouse. So a tab cannot be clicked from inside pass-through.
// Accepted rather than worked around: leaving pass-through is a prerequisite
// for doing anything with the tabs anyway, and the callback carries no
// coordinates with which to special-case the title row.
func (gui *Gui) clickOutputTab(index int) error {
	if index < 0 || index >= outputTabCount {
		return nil
	}

	gui.setTab(outputTab(index))

	return nil
}
