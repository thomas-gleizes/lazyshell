package gui

import (
	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazycore/pkg/boxlayout"
)

const (
	sessionsViewName = "sessions"
	outputViewName   = "output"
	statusViewName   = "status"

	// sessionsWidthLandscape/sessionsHeightPortrait are Box.Size for the
	// sessions panel: a width in landscape (parent direction COLUMN), a
	// height in portrait (parent direction ROW) — the same Size field means a
	// different axis depending on which direction its parent picked.
	sessionsWidthLandscape = 30
	sessionsHeightPortrait = 10
	statusBarHeight        = 1
)

// isPortrait matches ROADMAP.md's threshold: narrow and tall enough that a
// side-by-side split would leave the output panel too thin to be useful.
func isPortrait(width, height int) bool {
	return width <= 84 && height > 45
}

// rootBox describes the whole screen: sessions+output above a status bar,
// with the sessions/output split flipping from side-by-side to stacked in
// portrait mode. boxlayout has no built-in orientation switch — this is done
// entirely via the Conditional* callbacks, evaluated with the size actually
// assigned to this box at layout time.
func rootBox() *boxlayout.Box {
	content := &boxlayout.Box{
		Weight: 1,
		ConditionalDirection: func(width, height int) boxlayout.Direction {
			if isPortrait(width, height) {
				return boxlayout.ROW
			}

			return boxlayout.COLUMN
		},
		ConditionalChildren: func(width, height int) []*boxlayout.Box {
			sessionsSize := sessionsWidthLandscape
			if isPortrait(width, height) {
				sessionsSize = sessionsHeightPortrait
			}

			return []*boxlayout.Box{
				{Window: sessionsViewName, Size: sessionsSize},
				{Window: outputViewName, Weight: 1},
			}
		},
	}

	return &boxlayout.Box{
		Direction: boxlayout.ROW,
		Children: []*boxlayout.Box{
			content,
			{Window: statusViewName, Size: statusBarHeight},
		},
	}
}

// layout is gocui's redraw/resize callback: it arranges sessions/output/status
// per rootBox and creates each view on first sight. It replaces the phase 0
// single-view layout entirely.
func (gui *Gui) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	if maxX < 2 || maxY < 2 {
		return nil
	}

	dimensions := boxlayout.ArrangeWindows(rootBox(), 0, 0, maxX, maxY)

	for _, name := range []string{sessionsViewName, outputViewName, statusViewName} {
		dim, ok := dimensions[name]
		if !ok {
			// A box with no width/height left ends up dropped by boxlayout;
			// nothing to draw for it this frame.
			continue
		}

		view, err := g.SetView(name, dim.X0, dim.Y0, dim.X1, dim.Y1, 0)
		if err != nil {
			if !goerrors.Is(err, gocui.ErrUnknownView) {
				return err
			}

			gui.initView(name, view)
		}
	}

	if g.CurrentView() == nil {
		if _, err := g.SetCurrentView(sessionsViewName); err != nil {
			return err
		}
	}

	return nil
}

// initView configures a view the first time it is created. Called once per
// view, from layout.
func (gui *Gui) initView(name string, view *gocui.View) {
	switch name {
	case sessionsViewName:
		view.Title = " sessions "
		view.Highlight = true
		view.HighlightInactive = true
		view.SelBgColor = gocui.ColorBlue
		gui.focus.onFocus[sessionsViewName] = func() { view.HighlightInactive = false }
		gui.focus.onFocusLost[sessionsViewName] = func() { view.HighlightInactive = true }
	case outputViewName:
		view.Title = " output "
		// The view mirrors a fixed-size emulated screen (pkg/screen): no
		// wrapping, no autoscroll, it is overwritten wholesale on every
		// render rather than appended to.
		view.Wrap = false
		view.Autoscroll = false
	case statusViewName:
		view.Frame = false
		gui.renderStatus(view)
	}
}
