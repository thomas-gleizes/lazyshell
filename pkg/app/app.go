// Package app wires the pieces of lazyshell together: configuration, the
// session manager and the GUI. It owns the bootstrap sequence and nothing else.
package app

import (
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// App is the top-level object of lazyshell. It will later also hold the user
// configuration (phase 5).
type App struct {
	sessions *session.Manager
	gui      *gui.Gui
}

// New builds the application without touching the terminal.
func New() *App {
	sessions := session.NewManager()

	return &App{sessions: sessions, gui: gui.New(sessions)}
}

// Run starts the GUI and blocks until the user quits. The terminal is
// restored before it returns, whatever the outcome. Every session is killed
// before Run returns: there is no detach in the MVP, everything dies with
// lazyshell.
func (a *App) Run() error {
	defer a.sessions.Shutdown()

	return a.gui.Run()
}
