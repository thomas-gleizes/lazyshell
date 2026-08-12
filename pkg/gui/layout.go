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

	// sessionsWidthLandscape/sessionsHeightPortrait are the default Box.Size for
	// the sessions panel: a width in landscape (parent direction COLUMN), a
	// height in portrait (parent direction ROW) — the same Size field means a
	// different axis depending on which direction its parent picked. Both are
	// overridden by pkg/config (sessions_panel_width/_height).
	sessionsWidthLandscape = 40
	sessionsHeightPortrait = 10
	statusBarHeight        = 1

	// portraitMaxWidth/portraitMinHeight are the default thresholds of
	// isPortrait, overridden by pkg/config.
	portraitMaxWidth  = 84
	portraitMinHeight = 45
)

// isPortrait matches ROADMAP.md's threshold: narrow and tall enough that a
// side-by-side split would leave the output panel too thin to be useful. The
// thresholds are configurable because "too thin to be useful" depends on the
// font and the terminal, which lazyshell cannot see.
func (gui *Gui) isPortrait(width, height int) bool {
	maxWidth, minHeight := gui.portraitMaxWidth, gui.portraitMinHeight
	if maxWidth <= 0 {
		maxWidth = portraitMaxWidth
	}

	if minHeight <= 0 {
		minHeight = portraitMinHeight
	}

	return width <= maxWidth && height > minHeight
}

// rootBox describes the whole screen: sessions+output above a status bar,
// with the sessions/output split flipping from side-by-side to stacked in
// portrait mode. boxlayout has no built-in orientation switch — this is done
// entirely via the Conditional* callbacks, evaluated with the size actually
// assigned to this box at layout time.
func (gui *Gui) rootBox() *boxlayout.Box {
	sessionsWidth := gui.sessionsPanelWidth
	if sessionsWidth <= 0 {
		sessionsWidth = sessionsWidthLandscape
	}

	sessionsHeight := gui.sessionsPanelHeight
	if sessionsHeight <= 0 {
		sessionsHeight = sessionsHeightPortrait
	}

	content := &boxlayout.Box{
		Weight: 1,
		ConditionalDirection: func(width, height int) boxlayout.Direction {
			if gui.isPortrait(width, height) {
				return boxlayout.ROW
			}

			return boxlayout.COLUMN
		},
		ConditionalChildren: func(width, height int) []*boxlayout.Box {
			// Zoom is a special case of this same box, not a second layout
			// tree: the sessions panel is simply left out of the children
			// boxlayout.ArrangeWindows lays out, so it gets no dimensions and
			// layout's own SetView loop leaves it undrawn for this frame.
			if gui.zoomed {
				return []*boxlayout.Box{
					{Window: outputViewName, Weight: 1},
				}
			}

			sessionsSize := sessionsWidth
			if gui.isPortrait(width, height) {
				sessionsSize = sessionsHeight
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

	dimensions := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, maxX, maxY)

	for _, name := range []string{sessionsViewName, outputViewName, statusViewName} {
		dim, ok := dimensions[name]
		if !ok {
			// A box with no width/height left ends up dropped by boxlayout —
			// nothing to arrange for it this frame. That alone would not hide
			// it though: gocui draws every registered view every frame at its
			// last known bounds, SetView'd this pass or not, so it has to be
			// told explicitly that it is off.
			if view, err := g.View(name); err == nil {
				view.Visible = false
			}

			continue
		}

		view, err := g.SetView(name, dim.X0, dim.Y0, dim.X1, dim.Y1, 0)
		if err != nil {
			if !goerrors.Is(err, gocui.ErrUnknownView) {
				return err
			}

			gui.initView(name, view)
		}

		// gocui draws every registered view every frame regardless of whether
		// SetView touched it this pass — a view simply left out of
		// dimensions (the sessions panel while zoomed) would otherwise still
		// be drawn at its last known bounds instead of disappearing. Visible
		// is what actually hides it; the sessions panel is the only view
		// that can ever be left out this way, so it is the only one that
		// needs re-arming here.
		view.Visible = true

		// Refreshed on every layout pass, not just at creation: the footer
		// depends on the panel's current width (it truncates to fit) and, for
		// the output panel, on whether pass-through or a full-screen
		// application is active.
		// InnerWidth, not Size: Size counts the frame columns, and gocui's
		// drawListFooter starts at x1-1-len(footer) and gives up silently when
		// that lands left of x0 — a footer one column too wide vanishes
		// entirely instead of being clipped.
		view.Footer = gui.panelFooter(name, view.InnerWidth())

		if name == sessionsViewName {
			// Cached because the group headers' rule fills the panel's width,
			// and renderSessionsPanel builds them from goEvery's background
			// goroutine — where reading view.InnerWidth(), a plain field read
			// gocui's own render pass writes, would be a race. Written here on
			// the GUI goroutine, read under the same mutex.
			gui.setSessionsHeaderWidth(view.InnerWidth())
		}

		// Same reasoning as the footer, and the same reason it cannot be done
		// once in initView: gocui truncates a tab strip that does not fit
		// instead of shortening it, so which set of labels is right depends on
		// the width this pass just assigned.
		if name == outputViewName {
			if gui.selectedSession() == nil {
				// The three tabs are three views of a session there is none of,
				// so the strip is dropped rather than left pointing at nothing;
				// gocui falls back to View.Title when Tabs is empty, which is
				// what puts the plain frame title up in its place.
				view.Tabs = nil
				view.Title = " lazyshell "

				// Re-rendered on every pass, not once when the panel emptied:
				// the welcome block is centered on the panel's current size,
				// and a resize is only known here.
				gui.renderWelcome(view)
			} else {
				view.Tabs = gui.tabLabels(tabStripWidth(view))
			}
		}
	}

	if g.CurrentView() == nil {
		if _, err := g.SetCurrentView(sessionsViewName); err != nil {
			return err
		}
	}

	gui.propagateResize(g)
	gui.reflowOutputOnResize()

	// Last, and after every layout view has been placed: the debug overlay is
	// not a box in the tree, it is drawn on top of the output panel and reads
	// that panel's freshly computed frame to know where to sit. Creating it
	// here also puts it last in gocui's view list, which is what keeps it
	// above the output panel on every subsequent frame.
	return gui.renderDebugPanel(g)
}

// reflowOutputOnResize restarts the output render task when the panel's width
// changes, because the resources tab draws its charts to a width captured when
// the task started and has no other way to hear about a resize.
//
// Guarded on an actual change, and on that tab only: restarting the task on
// every layout pass would throw away its previous-frame comparison ~33 times a
// second and repaint the whole screen with it.
func (gui *Gui) reflowOutputOnResize() {
	width := gui.outputPanelWidth()
	if width == gui.lastOutputWidth {
		return
	}

	// Logged from here rather than from propagateResize, which runs on every
	// layout pass: this is the one place that already knows the panel's width
	// actually changed, so the log gets one line per resize instead of ~33 a
	// second.
	gui.debug.Event("output panel width %d → %d", gui.lastOutputWidth, width)

	gui.lastOutputWidth = width

	if gui.outputTab == tabResources {
		gui.restartOutput()
	}
}

// toggleZoom flips the output panel between its normal share of the screen
// and full-screen, hiding the sessions panel — a case rootBox already handles
// as a flag, not a second layout tree. Bound on both the sessions view (the
// common case, "z" to zoom in) and matched by hand inside editDuringScroll
// for the output view (the only way back out, since the sessions view no
// longer exists to receive its own binding once zoomed).
func (gui *Gui) toggleZoom(g *gocui.Gui, _ *gocui.View) error {
	gui.zoomed = !gui.zoomed

	gui.debug.Event("zoom now %t", gui.zoomed)

	if gui.zoomed {
		_, err := g.SetCurrentView(outputViewName)

		return err
	}

	_, err := g.SetCurrentView(sessionsViewName)

	return err
}

// propagateResize keeps every session's pty and emulator aligned with the
// output panel's current size — not just the selected one, so whichever
// session gets selected next is already correctly sized instead of jumping
// at the moment of the switch. Session.Resize no-ops when the size has not
// changed, so calling this on every layout pass stays cheap.
func (gui *Gui) propagateResize(g *gocui.Gui) {
	view, err := g.View(outputViewName)
	if err != nil {
		return
	}

	// InnerSize, not Size: Size counts the frame's two border rows/columns,
	// which are not part of what actually gets drawn — showOutput writes the
	// emulator's rendered frame into the view wholesale, unwrapped, at
	// origin (0,0), with no scrolling of its own (see output.go's
	// drawCursor). Sizing the pty to Size() told the shell it had two more
	// rows than the panel can show: harmless for a single-line prompt, but
	// the bottom of anything that actually uses the full height — a
	// multi-line prompt, vim, htop — landed past the frame and never got
	// drawn, indistinguishable from being hidden by the interface.
	cols, rows := view.InnerSize()
	if cols <= 0 || rows <= 0 {
		return
	}

	for _, sess := range gui.sessions.List() {
		_ = sess.Resize(cols, rows)
	}
}

// initView configures a view the first time it is created. Called once per
// view, from layout.
func (gui *Gui) initView(name string, view *gocui.View) {
	switch name {
	case sessionsViewName:
		view.Title = " sessions "
		view.Highlight = true
		view.HighlightInactive = true
		view.SelBgColor = gui.theme.SelectedBgColor
		gui.focus.onFocus[sessionsViewName] = func() { view.HighlightInactive = false; gui.updateBorderColor() }
		gui.focus.onFocusLost[sessionsViewName] = func() { view.HighlightInactive = true; gui.updateBorderColor() }
	case outputViewName:
		// Tabs, not Title: gocui's drawTitle falls back to Title only when
		// Tabs is empty, so setting both would leave a dead string behind.
		// The strip itself is (re)built on every layout pass below, since it
		// depends on the panel's current width. SelFgColor is what marks the
		// active tab — see Theme.TabActiveColor.
		view.TabIndex = int(gui.outputTab)
		view.SelFgColor = gui.theme.TabActiveColor
		// The view mirrors a fixed-size emulated screen (pkg/screen): no
		// wrapping, no autoscroll, it is overwritten wholesale on every
		// render rather than appended to.
		view.Wrap = false
		view.Autoscroll = false
		// Editable is set once, permanently: gocui always lets a view-scoped
		// SetKeybinding win before consulting the Editor, so toggling this on
		// entry to pass-through would not help — editOutput must own every
		// keystroke on this view from the start and decide for itself what to
		// swallow, forward to the shell, or let fall through.
		view.Editable = true
		view.Editor = gocui.EditorFunc(gui.editOutput)
		// The terminal cursor is drawn on this panel only, and only while it
		// has focus. Losing focus must take it away immediately rather than at
		// the next frame the session happens to change, and regaining focus
		// must restart the render task so the very next tick redraws it — see
		// showOutput's skip-if-unchanged rule.
		gui.focus.onFocus[outputViewName] = func() { gui.restartOutput(); gui.updateBorderColor() }
		gui.focus.onFocusLost[outputViewName] = func() { gui.g.Cursor = false; gui.updateBorderColor() }
	case statusViewName:
		view.Frame = false
		gui.renderStatus(view)
	}
}
