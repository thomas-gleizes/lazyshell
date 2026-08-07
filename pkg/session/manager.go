package session

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/hook"
	"github.com/thomas-gleizes/lazyshell/pkg/screen"
)

// DefaultKillTimeout is how long Kill waits for a session to die after
// SIGTERM before escalating to SIGKILL, and again after that before giving up.
//
// Kept short rather than generous: on at least one real target platform
// (WSL2), SIGTERM delivery to a pty-owning process — by process group or by
// direct pid, same effect either way — becomes unreliable/delayed as soon as
// a second pty-owning session exists in the same process, even though it is
// near-instant with only one. SIGKILL always still lands, so a short timeout
// just bounds how long a "kill" keypress can visibly hang for, rather than
// trying to fix signal delivery itself (out of userspace's control).
const DefaultKillTimeout = 2 * time.Second

// DefaultTerm is the TERM value sessions are started with unless Manager.Term
// says otherwise: the bundled emulator (pkg/screen) implements 256-colour
// xterm, and announcing anything less makes shells and editors degrade for no
// reason.
const DefaultTerm = "xterm-256color"

// Manager owns every session's lifecycle: creation, lookup, listing and
// killing. It is deliberately independent from any future TaskManager, which
// will only own display/reading goroutines, never the shell processes
// themselves.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	order    []string
	nextID   atomic.Uint64

	// KillTimeout is exported so tests can shrink it instead of waiting on a
	// production-sized timeout.
	KillTimeout time.Duration

	// ScrollbackSize is the terminal emulator's scrollback size (pkg/config's
	// ScrollbackSize), in lines. Zero means "use the emulator's own default"
	// (vt.DefaultScrollbackSize) — NewManager's zero value is deliberately
	// usable as-is by every existing test.
	ScrollbackSize int

	// Term is the TERM value sessions are started with (pkg/config's Term).
	// Empty means DefaultTerm.
	Term string

	// Detector classifies a session's foreground process into an AI agent
	// state (pkg/agent), consulted from each session's drain goroutine. Nil
	// means "no manifests loaded" — every session then reports
	// agent.StateNone, the same as a session running no known agent; this is
	// NewManager's zero value so every existing test keeps working unchanged.
	Detector *agent.Detector
}

// NewManager returns an empty Manager, ready to create sessions.
func NewManager() *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		KillTimeout: DefaultKillTimeout,
	}
}

// Options describes a session to create. It exists because the declarative
// project config (phase 6) needs two things the positional constructors cannot
// express — extra environment variables and a command to run on startup —
// without growing a fourth and fifth positional argument on every call site.
type Options struct {
	// Name is the session's display name.
	Name string
	// Shell is the command started behind the pty.
	Shell string
	// Cwd is the shell's working directory. Empty means the process's own.
	Cwd string
	// Env adds to (and overrides within) the inherited environment.
	Env map[string]string
	// Command, when non-empty, is typed into the session once it is up — see
	// NewWithOptions for why it is injected rather than exec'd.
	Command string
}

// New starts shell behind a pty, in the current working directory, and
// registers it under the given name. It is a thin wrapper around
// NewWithOptions for the common case.
func (m *Manager) New(name, shell string) (*Session, error) {
	return m.NewWithOptions(Options{Name: name, Shell: shell})
}

// NewInDir is New with an explicit working directory — the "session dans un
// cwd choisi" ergonomics feature.
func (m *Manager) NewInDir(name, shell, cwd string) (*Session, error) {
	return m.NewWithOptions(Options{Name: name, Shell: shell, Cwd: cwd})
}

// NewWithOptions is the full session constructor. The session's drain goroutine
// is started before it returns, so no output is lost from the moment the shell
// is up.
func (m *Manager) NewWithOptions(opts Options) (*Session, error) {
	sess, err := m.newSession(fmt.Sprintf("session-%d", m.nextID.Add(1)), opts)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[sess.ID] = sess
	m.order = append(m.order, sess.ID)
	m.mu.Unlock()

	return sess, nil
}

// Restart re-creates an exited session with the same id and the Options it
// was originally started with — same name, shell, cwd, env and initial
// command. The session that just exited cannot be reused directly: killOnce
// means Kill (and the exit that already happened) can only ever run once on
// a given *Session*, so this spawns a fresh one and swaps it in under the
// same id, keeping the session's position in List().
func (m *Manager) Restart(id string) (*Session, error) {
	m.mu.RLock()
	old, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session: unknown id %q", id)
	}

	if old.Status() != StatusExited {
		return nil, fmt.Errorf("session %s: still running, cannot restart", id)
	}

	sess, err := m.newSession(id, old.opts)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// newSession is the shared session constructor behind NewWithOptions and
// Restart: it starts the process and its drain goroutine under the given id,
// but does not touch m.sessions/m.order — callers decide whether that means
// inserting a new entry or replacing an existing one.
//
// Options.Command is *typed into* the shell rather than exec'd in its place.
// Exec'ing would make the session go Exited the instant the command finishes —
// wrong for the case this exists for, a `npm run dev` you want to Ctrl-C and
// restart by hand. Injection keeps the shell underneath, exactly like
// `tmux send-keys`.
func (m *Manager) newSession(id string, opts Options) (*Session, error) {
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		if cwd, err = os.Getwd(); err != nil {
			return nil, fmt.Errorf("session: getwd: %w", err)
		}
	}

	sockPath := hook.SocketPath(id)

	cmd := exec.Command(opts.Shell)
	cmd.Env = buildEnv(m.term(), id, sockPath, opts.Env)
	cmd.Dir = cwd

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: defaultRows, Cols: defaultCols})
	if err != nil {
		return nil, fmt.Errorf("session: start %s: %w", opts.Shell, err)
	}

	sess := &Session{
		ID:        id,
		Cmd:       cmd,
		Cwd:       cwd,
		CreatedAt: time.Now(),
		opts:      opts,
		ptmx:      ptmx,
		screen:    m.newScreen(defaultCols, defaultRows),
		detector:  m.Detector,
		cols:      defaultCols,
		rows:      defaultRows,
		done:      make(chan struct{}),
	}
	sess.SetName(opts.Name)

	// A hook listener that fails to start (permission issue on
	// $XDG_RUNTIME_DIR, an exotic sandbox with no writable temp dir) leaves
	// hookServer nil rather than failing session creation: the authoritative
	// channel is one more source of a gutter hint, never something the rest
	// of lazyshell depends on. The 11a manifest-based fallback keeps working
	// either way.
	if srv, err := hook.Listen(sockPath, sess.SetAgentState); err == nil {
		sess.hookServer = srv
	}

	go sess.drain()

	if opts.Command != "" {
		// The pty's line discipline buffers this until the shell gets round to
		// reading, so there is no need to wait for a prompt first.
		if _, err := sess.Write([]byte(opts.Command + "\n")); err != nil {
			return sess, fmt.Errorf("session %s: injection de %q: %w", sess.ID, opts.Command, err)
		}
	}

	return sess, nil
}

// term is the TERM value to start sessions with: the configured one, else
// DefaultTerm — a value the bundled emulator actually implements.
func (m *Manager) term() string {
	if m.Term != "" {
		return m.Term
	}

	return DefaultTerm
}

// buildEnv is the child's environment: the process's own, TERM/LAZYSHELL_SESSION_ID/
// LAZYSHELL_SOCK forced, then the session's own overrides. The three forced
// entries come before extra: a session's identity and its hook channel are
// not something a project's declarative `env:` should be able to redefine.
// Sorted so two runs of the same config produce the same environment, which
// is what makes it testable.
func buildEnv(term, sessionID, sockPath string, extra map[string]string) []string {
	env := append(os.Environ(), "TERM="+term, "LAZYSHELL_SESSION_ID="+sessionID, "LAZYSHELL_SOCK="+sockPath)

	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}

	return env
}

// newScreen builds a session's terminal emulator, honouring ScrollbackSize
// when set.
func (m *Manager) newScreen(cols, rows int) *screen.Screen {
	if m.ScrollbackSize > 0 {
		return screen.NewWithScrollback(cols, rows, m.ScrollbackSize)
	}

	return screen.New(cols, rows)
}

// Kill terminates the session with the given id, per Session.Kill.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session: unknown id %q", id)
	}

	return sess.Kill(m.KillTimeout)
}

// Remove kills the session if it is still running, then drops it entirely
// from the manager — unlike Kill, the session no longer appears in List()
// afterwards and cannot be restarted. Used when the user wants a session
// gone from the panel rather than just stopped.
func (m *Manager) Remove(id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session: unknown id %q", id)
	}

	if sess.Status() != StatusExited {
		if err := sess.Kill(m.KillTimeout); err != nil {
			return err
		}
	}

	m.mu.Lock()
	delete(m.sessions, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)

			break
		}
	}
	m.mu.Unlock()

	return nil
}

// List returns every session in creation order, including exited ones: they
// stay visible, the same way a stopped container stays listed in lazydocker.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			out = append(out, s)
		}
	}

	return out
}

// Get looks up a session by id.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[id]

	return s, ok
}

// Shutdown kills every session and waits for all of them to fully terminate,
// so a caller (pkg/app on quit) knows it can exit the process without leaving
// orphaned children behind. There is no detach in the MVP: everything dies
// with lazyshell.
func (m *Manager) Shutdown() {
	sessions := m.List()

	for _, s := range sessions {
		go func(s *Session) { _ = s.Kill(m.KillTimeout) }(s)
	}

	for _, s := range sessions {
		<-s.Done()
	}
}
