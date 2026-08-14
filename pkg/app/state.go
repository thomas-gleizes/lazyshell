package app

import (
	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// restoreState creates one session per entry of a saved layout (config.LoadState),
// in the order it was saved. Same "one bad entry must not cost the others"
// treatment as autostart, and the same reason: this runs at startup, on data
// nobody has had a chance to review since it was written.
func restoreState(sessions *session.Manager, state *config.StateFile, shell string) []error {
	var errs []error

	for _, spec := range state.Sessions {
		_, err := sessions.NewWithOptions(session.Options{
			Name:    spec.Name,
			Group:   spec.Group,
			Shell:   shell,
			Cwd:     spec.Cwd,
			Command: spec.Command,
		})
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// snapshotSessions turns the manager's current sessions into the layout
// SaveState persists — name, group, cwd and *launch-time* command (Session.
// Command(), not anything the shell has cd'd to or run since), matching the
// roadmap's own scope for this feature.
func snapshotSessions(sessions []*session.Session) []config.StateSession {
	if len(sessions) == 0 {
		return nil
	}

	out := make([]config.StateSession, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, config.StateSession{
			Name:    sess.Name(),
			Group:   sess.Group(),
			Cwd:     sess.Cwd,
			Command: sess.Command(),
		})
	}

	return out
}
