// Package hook is lazyshell's authoritative AI agent state channel: a Unix
// socket per session that an agent's own hook mechanism (Claude Code's
// settings.json hooks, Codex's notify command, ...) can push a state to,
// via the `lazyshell hook <event>` CLI command. lazyshell never calls the
// agent — this package only ever listens.
//
// The wire protocol is deliberately the smallest thing that works: one line
// per event, the line being one of agent.State's four spellings
// ("idle"/"working"/"blocked"/"done"). No JSON, no framing, no verbs beyond
// "this is my state now" — an agent declares itself, it does not control
// lazyshell (see the design report's open question on a control API).
package hook

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

// dialTimeout bounds how long a hook invocation (a short-lived CLI process,
// typically blocking the agent that called it) waits on a socket that turns
// out to be gone or unresponsive.
const dialTimeout = 500 * time.Millisecond

// runtimeDir is where every session's hook socket lives:
// $XDG_RUNTIME_DIR/lazyshell/<pid>, falling back to os.TempDir()'s
// equivalent when $XDG_RUNTIME_DIR is unset — same "always land somewhere,
// never fail silently" precedence pkg/config.Path uses for the config file.
// The pid segment is this lazyshell process's own, so two instances never
// collide on the same session id: creation order alone ("session-1") is not
// unique across processes.
func runtimeDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}

	return filepath.Join(base, "lazyshell", strconv.Itoa(os.Getpid()))
}

// SocketPath is the Unix socket path a session's hook channel listens on —
// exported so pkg/session can compute it before starting the process, to
// inject it into that process's environment as $LAZYSHELL_SOCK. Kept short
// on purpose (session ids are short, "session-N"): a Unix socket path is
// capped at roughly 100 bytes depending on the platform, and $TMPDIR-based
// fallbacks in particular can already eat into that budget.
func SocketPath(sessionID string) string {
	return filepath.Join(runtimeDir(), sessionID+".sock")
}

// Server is one session's hook listener.
type Server struct {
	ln   net.Listener
	path string
}

// Listen opens path (creating its parent directory at 0700) and starts
// accepting connections in the background, calling onEvent once per valid
// line received on any of them. A line that does not parse as an
// agent.State is silently dropped, never fatal to the connection or the
// listener — a malformed hook payload degrades a marker, nothing else.
//
// Both shapes of client work: one connection per event (the `lazyshell
// hook` CLI, which dials, writes one line and disconnects) and a
// long-lived connection streaming several events over time.
func Listen(path string, onEvent func(agent.State)) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("hook: mkdir: %w", err)
	}

	// A socket file left behind by an unclean previous exit would otherwise
	// make Listen fail with "address already in use" — every Unix socket
	// server has to clear this before binding.
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("hook: listen %s: %w", path, err)
	}

	// The socket is readable by any process the session's environment
	// reaches (that is the whole point — $LAZYSHELL_SOCK), but it must not
	// be readable by anyone else on the machine.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()

		return nil, fmt.Errorf("hook: chmod %s: %w", path, err)
	}

	s := &Server{ln: ln, path: path}

	go s.acceptLoop(onEvent)

	return s, nil
}

func (s *Server) acceptLoop(onEvent func(agent.State)) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// Only ever returns once Close has torn down the listener.
			return
		}

		go handleConn(conn, onEvent)
	}
}

func handleConn(conn net.Conn, onEvent func(agent.State)) {
	defer func() { _ = conn.Close() }()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if state, ok := agent.ParseState(line); ok {
			onEvent(state)
		}
	}
}

// Close stops accepting new connections and removes the socket file, so a
// session that exits never leaves a stale entry under $XDG_RUNTIME_DIR.
// Connections already accepted are left to finish and close on their own.
func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.path)

	return err
}

// Send dials the socket at path and writes state as a single line — the
// client half, used by `lazyshell hook <event>`. Bounded by dialTimeout in
// both directions: a hook invocation typically blocks the agent that called
// it, and lazyshell being slow to accept (or gone entirely) must not turn
// into the agent hanging.
func Send(path string, state agent.State) error {
	conn, err := net.DialTimeout("unix", path, dialTimeout)
	if err != nil {
		return fmt.Errorf("hook: dial %s: %w", path, err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetWriteDeadline(time.Now().Add(dialTimeout)); err != nil {
		return fmt.Errorf("hook: set write deadline: %w", err)
	}

	if _, err := conn.Write([]byte(state.String() + "\n")); err != nil {
		return fmt.Errorf("hook: write: %w", err)
	}

	return nil
}
