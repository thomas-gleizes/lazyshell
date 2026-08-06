// Package app wires the pieces of lazyshell together: configuration, the
// session manager and the GUI. It owns the bootstrap sequence and nothing else.
package app

import (
	"fmt"
	"os"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// App is the top-level object of lazyshell.
type App struct {
	sessions *session.Manager
	gui      *gui.Gui
}

// New builds the application without touching the terminal. A malformed
// config file is reported on stderr and falls back to defaults rather than
// preventing lazyshell from starting — the terminal is not yet taken over at
// this point, so it is still safe to print directly.
func New() *App {
	cfg, err := config.Load(config.Path())
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazyshell: %v (using defaults)\n", err)

		cfg = config.Default()
	}

	sessions := session.NewManager()
	sessions.ScrollbackSize = cfg.ScrollbackSize

	return &App{sessions: sessions, gui: gui.New(sessions, cfg)}
}

// Run starts the GUI and blocks until the user quits. The terminal is
// restored before it returns, whatever the outcome. Every session is killed
// before Run returns: there is no detach in the MVP, everything dies with
// lazyshell.
func (a *App) Run() error {
	defer a.sessions.Shutdown()

	return a.gui.Run()
}
