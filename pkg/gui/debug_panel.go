package gui

import (
	"fmt"
	"strings"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/rivo/uniseg"

	"github.com/thomas-gleizes/lazyshell/pkg/debug"
)

const debugViewName = "debug"

// The debug panel's preferred inner size, and the smallest it is worth
// drawing at. Below the minimum the panel would cover the output panel without
// showing anything readable, so it is dropped entirely rather than squeezed —
// the file still has everything.
const (
	// Wide enough for the longest routine line — a keystroke with its raw
	// values and the mode it landed in — without clipping, which is the whole
	// difference between a readable tail and a teaser.
	debugPanelWidth     = 60
	debugPanelHeight    = 14
	debugPanelMinWidth  = 20
	debugPanelMinHeight = 4
)

// debugEntryTimeFormat is the panel's clock. Much shorter than pkg/debug's
// file format: on a view showing the last dozen seconds the date and the hour
// are noise, while the milliseconds are the signal — they are how you tell one
// keystroke's cascade of lines from the next one's. Every column spent here is
// a column not spent on the line's actual content.
const debugEntryTimeFormat = "04:05.000"

// debugPanelGeometry places the panel in the output panel's top-right corner,
// one column inside its frame, and returns the SetView corners for it.
//
// It is deliberately computed from the output view's *current* frame rather
// than from boxlayout: the panel is an overlay, not a box in the tree, so it
// has to follow whatever the layout decided this frame — zoom, portrait mode
// and resizes all come for free that way.
//
// ok is false when the output panel is too small to host it; the caller then
// removes the view instead of drawing a sliver.
func debugPanelGeometry(outX0, outY0, outX1, outY1 int) (x0, y0, x1, y1 int, ok bool) {
	// The output panel's inner span, i.e. what is left once its own frame
	// columns/rows are excluded, minus the one-cell inset on the side the
	// panel is anchored to.
	maxInnerWidth := outX1 - outX0 - 3
	maxInnerHeight := outY1 - outY0 - 3

	innerWidth := min(debugPanelWidth, maxInnerWidth)
	innerHeight := min(debugPanelHeight, maxInnerHeight)

	if innerWidth < debugPanelMinWidth || innerHeight < debugPanelMinHeight {
		return 0, 0, 0, 0, false
	}

	// gocui's corners are the frame itself, so a span of innerWidth usable
	// columns needs innerWidth+1 between x0 and x1.
	x1 = outX1 - 1
	x0 = x1 - innerWidth - 1
	y0 = outY0 + 1
	y1 = y0 + innerHeight + 1

	return x0, y0, x1, y1, true
}

// renderDebugPanel draws (or removes) the floating debug panel. Called at the
// end of gui.layout, on every frame, for two reasons: the view must be created
// *after* the layout's own views so gocui — which draws in creation order and
// runs with SupportOverlaps false — keeps it on top, and re-running SetView
// every frame is what makes it follow the output panel across resizes.
//
// Unlike help/prompt/confirm this popup never takes focus and registers no
// keybinding of its own: it is a read-only readout, and stealing the current
// view would break pass-through, which is precisely when it is most useful.
func (gui *Gui) renderDebugPanel(g *gocui.Gui) error {
	// Nil while a bare Gui{} literal is under test, the same guard setTab
	// carries: debugPanelVisible above is still the state of record, and the
	// next layout pass is what draws from it.
	if g == nil {
		return nil
	}

	if gui.debug == nil || !gui.debugPanelVisible {
		return deleteDebugPanel(g)
	}

	out, err := g.View(outputViewName)
	if err != nil || !out.Visible {
		return deleteDebugPanel(g)
	}

	x0, y0, x1, y1, ok := debugPanelGeometry(out.Dimensions())
	if !ok {
		return deleteDebugPanel(g)
	}

	view, err := g.SetView(debugViewName, x0, y0, x1, y1, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = gui.tr.T("debug.title")
		view.FrameColor = gui.theme.LockedBorderColor
	}

	// SetView on an existing view keeps its position in gocui's draw order,
	// so the frame colour set at creation still applies; only the content is
	// rebuilt. Visible has to be re-armed because deleteDebugPanel may have
	// run in between only in the delete sense — but a view left out of a
	// frame is still drawn, so this is also what makes the panel honest after
	// the output panel itself was hidden.
	view.Visible = true

	width, height := view.InnerWidth(), view.InnerHeight()

	view.Clear()
	fmt.Fprint(view, gui.debugPanelContent(gui.debug.Recent(height), width, height))

	return nil
}

// deleteDebugPanel removes the view if it exists. No DeleteViewKeybindings
// counterpart to the popups' teardown: this one never registered any.
func deleteDebugPanel(g *gocui.Gui) error {
	if _, err := g.View(debugViewName); err != nil {
		return nil
	}

	return g.DeleteView(debugViewName)
}

// debugPanelContent renders entries oldest-first, so the newest line sits at
// the bottom where a log is read. The caller has already asked pkg/debug for
// at most `height` of them.
func (gui *Gui) debugPanelContent(entries []debug.Entry, width, height int) string {
	if len(entries) == 0 {
		return truncateToWidth(gui.tr.T("debug.empty"), width)
	}

	if len(entries) > height {
		entries = entries[len(entries)-height:]
	}

	lines := make([]string, 0, len(entries))

	for _, entry := range entries {
		line := fmt.Sprintf("%s %s %s", entry.At.Format(debugEntryTimeFormat), entry.Kind, entry.Text)
		lines = append(lines, truncateToWidth(line, width))
	}

	return strings.Join(lines, "\n")
}

// truncateToWidth cuts s to at most width terminal columns. Measured with
// uniseg for the same reason panelFooter does (footer.go): a line that is
// wider than the view in *columns* wraps and pushes the oldest entry off the
// top, which on a panel whose whole job is to show the last N events is a lie.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}

	if uniseg.StringWidth(s) <= width {
		return s
	}

	var (
		b     strings.Builder
		shown int
	)

	for _, r := range s {
		w := uniseg.StringWidth(string(r))
		if shown+w > width {
			break
		}

		b.WriteRune(r)
		shown += w
	}

	return b.String()
}

// toggleDebugPanel is the toggle_debug action (F12). It hides and shows the
// panel only — the file keeps being written either way, which is the point:
// you can get the output panel back without losing the trace of what you do
// next.
//
// A no-op when --debug was not given, so the binding can be registered
// unconditionally and bindings() stays a constant list (see staticBindings).
func (gui *Gui) toggleDebugPanel(g *gocui.Gui, _ *gocui.View) error {
	if gui.debug == nil {
		return nil
	}

	gui.debugPanelVisible = !gui.debugPanelVisible

	gui.debug.Action("toggle_debug → panel %s", visibilityLabel(gui.debugPanelVisible))

	return gui.renderDebugPanel(g)
}

func visibilityLabel(visible bool) string {
	if visible {
		return "shown"
	}

	return "hidden"
}
