// Package gui owns everything terminal-facing: the gocui bootstrap, the
// layout, the keybindings and the rendering loop.
package gui

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/jesseduffield/gocui"
)

// mainViewName is the single view of phase 0. It is replaced by the
// sessions/output split in phase 3.
const mainViewName = "main"

// reRenderInterval is the rate at which we check whether a view has been
// written to and needs to be pushed to the screen. Same value as lazydocker.
const reRenderInterval = 30 * time.Millisecond

// Gui holds the gocui instance and the state of the interface.
type Gui struct {
	g *gocui.Gui

	// PauseBackgroundThreads stops the periodic tasks started by goEvery, for
	// when the terminal is handed over to another process.
	PauseBackgroundThreads bool
}

// New allocates the Gui. It does not touch the terminal: that only happens in
// Run.
func New() *Gui {
	return &Gui{}
}

// Run initialises gocui and blocks in the main loop until the user quits. The
// terminal is always restored before returning, including on panic.
func (gui *Gui) Run() (err error) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.OutputNormal,
		SupportOverlaps: false,
	})
	if err != nil {
		return fmt.Errorf("failed to initialise the terminal: %w", err)
	}

	// Deferred calls run last-in-first-out: g.Close() restores the terminal
	// first, then this handler turns a panic into a plain error the caller can
	// print on a sane terminal.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n\n%s", r, debug.Stack())
		}
	}()
	defer g.Close()

	gui.g = g
	g.Cursor = false
	g.Mouse = false

	g.SetManagerFunc(gui.layout)

	if err := gui.setKeybindings(g); err != nil {
		return err
	}

	gui.goEvery(reRenderInterval, gui.reRenderMain)

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}

	return nil
}

// layout is called by gocui on every redraw and every resize. In phase 0 it
// only maintains a single full-screen view; the boxlayout tree arrives in
// phase 3.
func (gui *Gui) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	if maxX < 2 || maxY < 2 {
		return nil
	}

	view, err := g.SetView(mainViewName, 0, 0, maxX-1, maxY-1, 0)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}

		// First creation only.
		view.Title = " lazyshell "
		view.Wrap = true
		view.Autoscroll = true
		fmt.Fprintln(view, "lazyshell — phase 0")
		fmt.Fprintln(view, "")
		fmt.Fprintln(view, "q / Ctrl-C : quitter")

		if _, err := g.SetCurrentView(mainViewName); err != nil {
			return err
		}
	}

	return nil
}

// reRenderMain asks gocui for a redraw when the main view has been written to
// since the last frame. Writes to a gocui.View are thread-safe but do not
// trigger a redraw by themselves, hence this tick.
func (gui *Gui) reRenderMain() error {
	view, err := gui.g.View(mainViewName)
	if err != nil {
		// The view does not exist yet; nothing to render.
		return nil
	}

	if view.IsTainted() {
		gui.g.Update(func(*gocui.Gui) error { return nil })
	}

	return nil
}

// goEvery runs fn once, then on every tick of interval until the process
// exits. Ported from lazydocker's helper of the same name.
func (gui *Gui) goEvery(interval time.Duration, fn func() error) {
	_ = fn()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if !gui.PauseBackgroundThreads {
				_ = fn()
			}
		}
	}()
}
