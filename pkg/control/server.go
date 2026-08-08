package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// maxRequestBytes bounds one request line. A Request carries a command line
// and a chunk of text to type, never a file, so anything past this is either a
// bug or an attempt to make lazyshell allocate; bufio's own default of 64 KiB
// would do, but saying it here makes the limit a decision rather than a
// library detail.
const maxRequestBytes = 256 * 1024

// Server is the control socket's listener. There is one per lazyshell process
// — see config.ControlSocketPath.
type Server struct {
	ln   net.Listener
	path string
}

// Listen opens path (creating its parent directory at 0700) and starts serving
// requests in the background, dispatching each to h.
//
// The socket is created at 0600, which is the entire access control this API
// has: no token, no handshake, nothing in the protocol identifies the caller.
// Every process running as this user can therefore drive lazyshell once the
// feature is on, which is why config.Control.Enabled defaults to false and why
// Listen is only ever called behind it.
//
// A malformed or unknown request is answered with an error Response and the
// connection stays open — the same "degrade, never fail hard" rule pkg/hook
// follows, for the same reason: an agent that gets a verb wrong must not lose
// the channel over it.
func Listen(path string, h Handler) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("control: mkdir: %w", err)
	}

	// A socket file left behind by an unclean previous exit would otherwise
	// make Listen fail with "address already in use".
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("control: listen %s: %w", path, err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()

		return nil, fmt.Errorf("control: chmod %s: %w", path, err)
	}

	s := &Server{ln: ln, path: path}

	go s.acceptLoop(h)

	return s, nil
}

// Path is the socket the server is listening on, so a caller that let Listen
// derive it can report it.
func (s *Server) Path() string {
	return s.path
}

func (s *Server) acceptLoop(h Handler) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// Only ever returns once Close has torn down the listener.
			return
		}

		go serveConn(conn, h)
	}
}

// serveConn reads requests until the client hangs up, answering each with
// exactly one response line. Both shapes of client work: one connection per
// request (what `lazyshell ctl` does — dial, one request, read the answer,
// disconnect) and a long-lived connection issuing several.
func serveConn(conn net.Conn, h Handler) {
	defer func() { _ = conn.Close() }()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), maxRequestBytes)

	enc := json.NewEncoder(conn)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var (
			req  Request
			resp Response
		)

		if err := json.Unmarshal(line, &req); err != nil {
			resp = Response{OK: false, Error: fmt.Sprintf("requête illisible : %v", err)}
		} else {
			resp = dispatch(h, req)
		}

		// Encode writes the object plus a newline, which is the framing.
		// A write failure means the client is gone; nothing left to say.
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// Close stops accepting new connections and removes the socket file, so a
// lazyshell that exits never leaves a stale entry under config.RuntimeDir.
// Connections already accepted are left to finish on their own.
func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.path)

	return err
}
