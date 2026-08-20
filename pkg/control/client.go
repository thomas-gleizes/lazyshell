package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// callTimeout bounds a `lazyshell ctl` invocation end to end. Deliberately
// larger than pkg/hook's 500 ms: a hook only has to be accepted, whereas
// VerbNew starts a pty and waits for a turn of the interface's event loop, and
// VerbKill waits for a process group to actually be reaped (session.Manager's
// KillTimeout is 2 s by default, twice over in the worst case). Three seconds
// is short enough that a lazyshell which has wedged still fails visibly rather
// than hanging the agent that called it.
const callTimeout = 3 * time.Second

// waitDeadlineMargin is added on top of a VerbWait request's own timeout to
// get the client's transport deadline (see callDeadline). It must be
// comfortably larger than the server's own poll granularity so the server
// always has time to notice its deadline and answer with a structured
// Response{OK:false} before the client's raw "i/o timeout" would otherwise
// fire on the exact same event — a strictly worse version of the same
// answer.
const waitDeadlineMargin = 5 * time.Second

// callDeadline is the transport deadline Call sets for req: callTimeout for
// every verb except VerbWait, which needs room for its own caller-supplied
// timeout instead — callTimeout's 3 s is sized for VerbNew/VerbKill, not for
// a request that is meant to block for minutes.
func callDeadline(req Request) time.Duration {
	if req.Verb != VerbWait {
		return callTimeout
	}

	return resolveWaitTimeout(req.Timeout) + waitDeadlineMargin
}

// IsLive reports whether something is actually accepting connections on path.
// Used to tell a live control socket from one a crashed lazyshell left behind:
// the file surviving proves nothing, only a successful dial does.
func IsLive(path string) bool {
	conn, err := net.DialTimeout("unix", path, dialProbeTimeout)
	if err != nil {
		return false
	}

	_ = conn.Close()

	return true
}

// dialProbeTimeout is deliberately much shorter than callTimeout: probing is a
// "is anyone there" question asked of several candidates in a row, and a dead
// socket answers immediately anyway (ECONNREFUSED). Only a pathological case —
// a socket file on a hung network filesystem — spends it.
const dialProbeTimeout = 200 * time.Millisecond

// Call sends one request to the control socket at path and returns the single
// response to it — the client half, used by `lazyshell ctl`.
//
// The returned error is about the *transport*: no socket, no answer, garbage
// on the wire. A verb that lazyshell understood and refused comes back as a
// Response with OK false and Error set, with a nil error here. Callers must
// check both. For VerbWait specifically, this distinction is what lets a
// legitimate timeout answer as the former (Response{OK:false}) rather than
// the latter — see callDeadline.
func Call(path string, req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", path, callTimeout)
	if err != nil {
		return Response{}, fmt.Errorf("control: dial %s: %w", path, err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(callDeadline(req))); err != nil {
		return Response{}, fmt.Errorf("control: set deadline: %w", err)
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("control: write: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), maxResponseBytes)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Response{}, fmt.Errorf("control: read: %w", err)
		}

		return Response{}, fmt.Errorf("control: %s a fermé la connexion sans répondre", path)
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return Response{}, fmt.Errorf("control: réponse illisible : %w", err)
	}

	return resp, nil
}

// maxResponseBytes bounds one response line. Much larger than
// maxRequestBytes because VerbRead legitimately returns a whole scrollback,
// which at the default 10 000 lines can run to several megabytes.
const maxResponseBytes = 32 * 1024 * 1024
