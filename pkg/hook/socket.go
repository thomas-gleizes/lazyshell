// Package hook is lazyshell's authoritative AI agent state channel: a Unix
// socket per session that an agent's own hook mechanism (Claude Code's
// settings.json hooks, Codex's notify command, ...) can push a state to,
// via the `lazyshell hook <event>` CLI command. lazyshell never calls the
// agent — this package only ever listens.
//
// The wire protocol is deliberately the smallest thing that works: one line
// per event, the line being one of agent.State's four spellings
// ("idle"/"working"/"blocked"/"done"). No JSON, no framing, no verbs beyond
// "this is my state now" — an agent declares itself here, it does not control
// lazyshell.
//
// That last part is a property of *this* channel, not of lazyshell as a whole
// any more: the verbs an agent can send live in pkg/control, on a separate
// socket with a separate protocol, off unless config.Control.Enabled says
// otherwise (docs/adr/0006-api-de-controle-par-les-agents.md). Keeping the two
// apart is the point — this one stays open by default precisely because it can
// only ever move a marker in a list.
package hook

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// dialTimeout bounds how long a hook invocation (a short-lived CLI process,
// typically blocking the agent that called it) waits on a socket that turns
// out to be gone or unresponsive.
const dialTimeout = 500 * time.Millisecond

// SocketPath is the Unix socket path a session's hook channel listens on —
// exported so pkg/session can compute it before starting the process, to
// inject it into that process's environment as $LAZYSHELL_SOCK. Kept short
// on purpose (session ids are short, "session-N"): config.RuntimeDir's own
// doc comment explains the path budget this spends from.
func SocketPath(sessionID string) string {
	return filepath.Join(config.RuntimeDir(), sessionID+".sock")
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
