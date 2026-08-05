package session

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"

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
}

// NewManager returns an empty Manager, ready to create sessions.
func NewManager() *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		KillTimeout: DefaultKillTimeout,
	}
}

// New starts shell behind a pty and registers it under the given name. The
// session's drain goroutine is started before New returns, so no output is
// lost from the moment the shell is up.
func (m *Manager) New(name, shell string) (*Session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("session: getwd: %w", err)
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Dir = cwd

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: defaultRows, Cols: defaultCols})
	if err != nil {
		return nil, fmt.Errorf("session: start %s: %w", shell, err)
	}

	sess := &Session{
		ID:        fmt.Sprintf("session-%d", m.nextID.Add(1)),
		Name:      name,
		Cmd:       cmd,
		Cwd:       cwd,
		CreatedAt: time.Now(),
		ptmx:      ptmx,
		screen:    screen.New(defaultCols, defaultRows),
		cols:      defaultCols,
		rows:      defaultRows,
		done:      make(chan struct{}),
	}

	go sess.drain()

	m.mu.Lock()
	m.sessions[sess.ID] = sess
	m.order = append(m.order, sess.ID)
	m.mu.Unlock()

	return sess, nil
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
