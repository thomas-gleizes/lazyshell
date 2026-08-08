package gui

import (
	"fmt"
	"math"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/control"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// onGUITimeout bounds how long a control verb waits for its turn on gocui's
// goroutine. Only reached if the event loop is wedged or not running at all,
// which is precisely when a caller must get an error instead of blocking:
// pkg/control's own callTimeout is 3 s, so this has to fail first for the
// message to be the useful one.
const onGUITimeout = 2 * time.Second

// Gui implements control.Handler. The methods below are the *only* place
// something outside lazyshell can act on a session rather than merely describe
// itself — pkg/hook's SetAgentState being the "describe itself" half — and
// they only exist at all when config.Control.Enabled turned the socket on. See
// docs/adr/0006-api-de-controle-par-les-agents.md.
//
// Each runs on one of pkg/control's per-connection goroutines, never on
// gocui's, which splits them in two:
//
//   - List, Read and Send touch only things that carry their own mutex —
//     session.Manager's map, pkg/screen's emulator, Session.Write's pty — and
//     so run inline.
//   - New and Rename go through onGUI: they spend gui.sessionCounter or repaint
//     the panel, both of which belong to gocui's goroutine.
//   - Kill is both. The killing runs inline because it is *slow* (see its own
//     comment); only the repaint crosses over.
//
// Two ways to get this wrong, and they fail differently. Doing GUI-owned work
// inline is a data race the tests will not reliably catch. Doing slow work
// inside onGUI freezes the interface and trips onGUI's own guard, which is what
// Kill did until a real kill took longer than two seconds.

// onGUI runs fn on gocui's goroutine and waits for its result. Returns an
// error rather than hanging if the event loop never gets to it.
func (gui *Gui) onGUI(fn func() error) error {
	if gui.g == nil {
		return fmt.Errorf("interface non démarrée")
	}

	errc := make(chan error, 1)

	gui.g.Update(func(*gocui.Gui) error {
		errc <- fn()

		return nil
	})

	select {
	case err := <-errc:
		return err
	case <-time.After(onGUITimeout):
		return fmt.Errorf("interface occupée : délai dépassé")
	}
}

// resolveSession finds a session by manager id ("session-3", the value of
// $LAZYSHELL_SESSION_ID) or, failing that, by exact display name. Ids first:
// they are unique, names are not — nothing stops two sessions being called
// "build", and a lookup that silently picked one of them would be a worse
// answer than making the caller use the id.
func (gui *Gui) resolveSession(idOrName string) (*session.Session, error) {
	if idOrName == "" {
		return nil, fmt.Errorf("session non précisée")
	}

	if sess, ok := gui.sessions.Get(idOrName); ok {
		return sess, nil
	}

	for _, sess := range gui.sessions.List() {
		if sess.Name() == idOrName {
			return sess, nil
		}
	}

	return nil, fmt.Errorf("session inconnue : %s", idOrName)
}

// List reports every session, in creation order, exited ones included — the
// same list the panel shows, so an agent and a human are looking at the same
// thing.
func (gui *Gui) List() []control.SessionInfo {
	sessions := gui.sessions.List()
	infos := make([]control.SessionInfo, 0, len(sessions))

	for _, sess := range sessions {
		info := control.SessionInfo{
			ID:     sess.ID,
			Name:   sess.Name(),
			Status: sess.Status().String(),
			Cwd:    sess.Cwd,
		}

		// StateNone is "not an agent session", and has no spelling on purpose
		// (see agent.ParseState) — it must not become the string "none".
		if state := sess.AgentState(); state != agent.StateNone {
			info.AgentState = state.String()
		}

		if sess.Status() == session.StatusExited {
			info.ExitCode = sess.ExitCode()
		}

		infos = append(infos, info)
	}

	gui.debug.Event("control: list (%d sessions)", len(infos))

	return infos
}

// Read returns a session's output as plain text — no SGR, no cursor
// positioning. tail limits it to the last N lines; zero means the whole
// scrollback, which is exactly what exportSession dumps to a file.
//
// This is the verb that hands over whatever a session has printed, secrets
// included and unmasked: env_tab.go's looksLikeSecret has no equivalent here,
// because a credential echoed into a shell is indistinguishable from any other
// text once it is on screen. That is a property of the feature, documented in
// the ADR, not an oversight to patch here.
func (gui *Gui) Read(idOrName string, tail int) (string, error) {
	sess, err := gui.resolveSession(idOrName)
	if err != nil {
		return "", err
	}

	gui.debug.Event("control: read %s (tail %d)", sess.ID, tail)

	if tail > 0 {
		return sess.Screen().PlainTail(tail), nil
	}

	return sess.Screen().TextRange(0, math.MaxInt), nil
}

// New creates a session and leaves the selection alone. Deliberately unlike
// the `n` binding, which also selects the new session and hands it the
// keyboard (selectNewlyCreatedSession, focusSelectedShell): that is right when
// the user pressed a key and wrong when a background agent did, since it would
// yank the cursor out of whatever the user was typing into.
func (gui *Gui) New(name, cwd, command string) (string, error) {
	var id string

	err := gui.onGUI(func() error {
		sess, err := gui.createSessionWithOptions(session.Options{
			Name:    name,
			Cwd:     cwd,
			Command: command,
		})
		if err != nil {
			return err
		}

		id = sess.ID

		gui.debug.Event("control: new %s (%s)", sess.ID, sess.Name())

		return gui.renderSessionsPanel()
	})

	return id, err
}

// Send writes text into a session's pty, exactly as if it had been typed. The
// caller supplies its own "\r" to press Enter: pkg/control's Request.Text is
// verbatim, on the same reasoning as tmux's send-keys.
func (gui *Gui) Send(idOrName, text string) error {
	sess, err := gui.resolveSession(idOrName)
	if err != nil {
		return err
	}

	if sess.Status() == session.StatusExited {
		return fmt.Errorf("session %s terminée : rien pour lire ce qui serait envoyé", sess.ID)
	}

	gui.debug.Event("control: send %s (%d octets)", sess.ID, len(text))

	if _, err := sess.Write([]byte(text)); err != nil {
		return fmt.Errorf("écriture dans %s : %w", sess.ID, err)
	}

	return nil
}

// Kill terminates a session's process and leaves it listed as exited — the
// semantics of the interface's own kill binding, not of its delete. Removing a
// session outright is not exposed: it is the one destructive act with no
// recovery, and an agent has no business taking it.
//
// Manager.Kill runs here, not inside onGUI: it waits for the process group to
// actually be reaped, escalating to SIGKILL after KillTimeout and waiting
// again — up to 4 s by default. The interface's own binding already refuses to
// run it on gocui's goroutine for that reason (killSession wraps it in
// runBusy, which is a background goroutine), and putting it there would blow
// onGUI's 2 s guard on a kill that is merely slow, not stuck. Only the repaint
// crosses over.
func (gui *Gui) Kill(idOrName string) error {
	sess, err := gui.resolveSession(idOrName)
	if err != nil {
		return err
	}

	gui.debug.Event("control: kill %s (%s)", sess.ID, sess.Name())

	if err := gui.sessions.Kill(sess.ID); err != nil {
		return err
	}

	return gui.onGUI(gui.renderSessionsPanel)
}

// Rename changes a session's display name. Cosmetic, like the interface's own
// rename: SetName does not touch the running shell.
func (gui *Gui) Rename(idOrName, name string) error {
	if name == "" {
		return fmt.Errorf("nom vide")
	}

	sess, err := gui.resolveSession(idOrName)
	if err != nil {
		return err
	}

	return gui.onGUI(func() error {
		gui.debug.Event("control: rename %s (%s -> %s)", sess.ID, sess.Name(), name)
		sess.SetName(name)

		return gui.renderSessionsPanel()
	})
}

// startControlServer opens the control socket, or reports why it could not.
// Returns nil when the feature is off, which is the default — the caller
// simply has nothing to close in that case.
//
// A socket that fails to open is a startup error, not a fatal one: an
// unwritable $XDG_RUNTIME_DIR must not be the reason a session manager refuses
// to run, the same rule pkg/app applies to every peripheral piece.
func (gui *Gui) startControlServer(path string) (*control.Server, error) {
	if !gui.controlEnabled {
		return nil, nil
	}

	srv, err := control.Listen(path, gui)
	if err != nil {
		return nil, err
	}

	gui.debug.Event("control: listening on %s", srv.Path())

	return srv, nil
}
