// Package app wires the pieces of lazyshell together: configuration, the
// session manager and the GUI. It owns the bootstrap sequence and nothing else.
package app

import "github.com/thomas-gleizes/lazyshell/pkg/gui"

// App is the top-level object of lazyshell. It will later also hold the
// session manager (phase 2) and the user configuration (phase 5).
type App struct {
	gui *gui.Gui
}

// New builds the application without touching the terminal.
func New() *App {
	return &App{gui: gui.New()}
}

// Run starts the GUI and blocks until the user quits. The terminal is restored
// before it returns, whatever the outcome.
func (a *App) Run() error {
	return a.gui.Run()
}
