package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/rivo/uniseg"
)

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
	// tabTerminal is the emulated screen — the default, and the only tab the
	// keyboard can be handed to a shell from.
	tabTerminal outputTab = iota
	// tabResources is the selected session's resource usage.
	tabResources
	// tabEnvironment is the environment its shell was launched with.
	tabEnvironment

	// outputTabCount bounds switchTab's wrap-around. Kept next to the values
	// it counts so adding a fourth tab is one edit, not two.
	outputTabCount = 3
)

// tabSeparator is what gocui joins the strip with. Not configurable — it is
// hardcoded in drawTitle — but tabLabels has to know its width to work out
// whether a set of labels fits.
const tabSeparator = " - "

// tabLabels is the strip gocui draws, in tab order, for a panel this wide.
//
// The width matters because gocui *truncates* a strip that does not fit rather
// than shortening it: the last tab simply loses its end, and on an 80-column
// terminal the output panel is only about 40 columns wide. So the full labels
// are used when they fit and an abbreviated set otherwise — the same
// width-dependent treatment layout already gives View.Footer, and the reason
// both are recomputed on every layout pass rather than once at creation.
//
// Each label is padded with a space on both sides. That is not cosmetic:
// gocui's drawTitle starts painting at x0+2 while GetClickedTabIndex starts
// counting at x0+1 (view.go), so a click lands one column left of where the
// label was drawn. The padding absorbs that off-by-one — without it, clicking
// the first character of a tab selects the one before it.
func (gui *Gui) tabLabels(width int) []string {
	// Tiers, widest first: the first one that fits wins, and the last is used
	// when nothing does. Abbreviation is spent where it costs the least
	// clarity — "env" first, since `env` is a command everybody already types
	// short, and only then the two words that carry real meaning.
	tiers := [][]string{
		{"tab.terminal", "tab.resources", "tab.environment"},
		{"tab.terminal", "tab.resources", "tab.environment_short"},
		{"tab.terminal_short", "tab.resources_short", "tab.environment_short"},
	}

	for _, keys := range tiers {
		labels := gui.paddedLabels(keys...)
		if width <= 0 || labelsWidth(labels) <= width {
			return labels
		}
	}

	return gui.paddedLabels(tiers[len(tiers)-1]...)
}

func (gui *Gui) paddedLabels(keys ...string) []string {
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		labels = append(labels, " "+gui.tr.T(key)+" ")
	}

	return labels
}

// labelsWidth is how many columns a strip of labels occupies once gocui has
// joined them. Measured in display columns rather than bytes, like footerText:
// an accented label is wider in bytes than on screen.
func labelsWidth(labels []string) int {
	total := 0

	for i, label := range labels {
		if i > 0 {
			total += uniseg.StringWidth(tabSeparator)
		}

		total += uniseg.StringWidth(label)
	}

	return total
}

// tabStripWidth is how many columns of a view's top frame line drawTitle can
// actually paint on: it starts at x0+2 and stops once past x1-2, which leaves
// two columns fewer than the view's inner width.
func tabStripWidth(view *gocui.View) int {
	return view.InnerWidth() - 2
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
