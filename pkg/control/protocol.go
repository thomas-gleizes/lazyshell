// Package control is lazyshell's agent control API: a Unix socket, one per
// lazyshell process, over which an AI agent running inside a session can list
// the other sessions, read their output, create new ones, type into them, kill
// and rename them — the `lazyshell ctl` command.
//
// It is the deliberate counterpart of pkg/hook, not an extension of it. The
// hook channel is inbound and declarative: an agent states its own state and
// the protocol has no vocabulary for anything else. This one carries verbs,
// and two of them (VerbNew, VerbSend) amount to running commands as the user.
// That is why it lives in its own package, on its own socket, with its own
// protocol, and stays off unless config.Control.Enabled turns it on — see
// docs/adr/0006-api-de-controle-par-les-agents.md for the decision this
// reverses and what it concedes.
//
// The wire protocol is one JSON object per line in each direction: a Request
// in, exactly one Response back, on the same connection, which may then carry
// further requests. Line-delimited JSON rather than hook's bare words because
// there is a payload to return (a list, a screen's worth of text, an error
// message) and because the verb set is expected to grow; a bufio.Scanner on
// both ends is the whole framing.
//
// This package knows nothing of gocui or pkg/session: the Handler interface is
// the seam, implemented by pkg/gui, which is the only layer that may touch the
// session manager and the interface's own goroutine.
package control

import "fmt"

// The verbs. Anything else is answered with an error Response, never a closed
// connection: an agent that guesses a verb name should learn that it guessed
// wrong, not lose the channel.
const (
	// VerbList reports every session — the only verb that takes no target.
	VerbList = "list"
	// VerbRead returns a session's output as plain text.
	VerbRead = "read"
	// VerbNew creates a session.
	VerbNew = "new"
	// VerbSend writes text into a session's pty, as if typed.
	VerbSend = "send"
	// VerbKill terminates a session's process, leaving it listed as exited —
	// the semantics of the interface's own kill, not of its delete.
	VerbKill = "kill"
	// VerbRename changes a session's display name.
	VerbRename = "rename"
	// VerbGroup moves a session into a group, or out of every group when
	// Request.Group is empty.
	VerbGroup = "group"
	// VerbGroupSend writes text into every session of a group.
	VerbGroupSend = "group-send"
	// VerbGroupKill terminates every session of a group, and reports how many
	// it touched in Response.Count.
	VerbGroupKill = "group-kill"
)

// Request is one line sent to the socket. Which fields matter depends on Verb;
// the rest are ignored rather than rejected, so a client built against a later
// version degrades to "that field did nothing" instead of an error.
type Request struct {
	Verb string `json:"verb"`
	// ID names the session a verb applies to, by manager id ("session-3", the
	// value of $LAZYSHELL_SESSION_ID) or by exact display name. Unused by
	// VerbList and VerbNew.
	ID string `json:"id,omitempty"`
	// Name is the session name to give (VerbNew) or to change to (VerbRename).
	Name string `json:"name,omitempty"`
	// Group is the group to put a session in (VerbNew, VerbGroup — empty means
	// ungrouped), the group to act on (VerbGroupSend, VerbGroupKill), or an
	// optional filter (VerbList). Empty on VerbList means "every session",
	// which is why the group verbs reject an empty Group themselves rather
	// than relying on this field alone to say what they mean.
	Group string `json:"group,omitempty"`
	// Cwd is the working directory a new session starts in. Empty means
	// lazyshell's own.
	Cwd string `json:"cwd,omitempty"`
	// Command is typed into the new session's shell, not exec'd in place of it
	// — the same semantics as session.Options.Command and tmux's send-keys, so
	// the shell survives the command.
	Command string `json:"command,omitempty"`
	// Text is what VerbSend writes into the session, verbatim. A trailing "\r"
	// is the caller's to add: pressing Enter is an explicit act.
	Text string `json:"text,omitempty"`
	// Tail limits VerbRead to the last N lines. Zero means the whole
	// scrollback.
	Tail int `json:"tail,omitempty"`
}

// Response is the single line sent back for each Request. OK is the field to
// branch on: the others are only meaningful when it is true, and Error only
// when it is false.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// ID is the id of the session VerbNew created.
	ID string `json:"id,omitempty"`
	// Output is VerbRead's text.
	Output string `json:"output,omitempty"`
	// Sessions is VerbList's answer.
	Sessions []SessionInfo `json:"sessions,omitempty"`
	// Count is how many sessions a group verb acted on. Only meaningful for
	// VerbGroupSend and VerbGroupKill; omitempty drops it everywhere else, and
	// a group verb that matched nothing errors rather than reporting 0, so the
	// ambiguity a plain int would carry here never arises.
	Count int `json:"count,omitempty"`
}

// SessionInfo is what VerbList reports about one session. Deliberately a flat
// copy rather than a reference to a session.Session: nothing on the far side of
// the socket gets to hold onto a live object.
type SessionInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Status is the process's state, "running" or "exited".
	Status string `json:"status"`
	// AgentState is the detected AI agent state ("idle"/"working"/"blocked"/
	// "done"), empty for a session running no known agent.
	AgentState string `json:"agent_state,omitempty"`
	// Group is the session's group, absent for an ungrouped one.
	Group string `json:"group,omitempty"`
	Cwd   string `json:"cwd,omitempty"`
	// ExitCode is meaningful only once Status is "exited", hence the pointer:
	// nil is "still running, ask again later", and it is the only shape that
	// works here. A plain int with omitempty drops the field for exit code
	// *zero* — the successful case — so a script could not tell a build that
	// passed from one still going, which is the single most likely thing to
	// ask this API. A plain int without omitempty is no better: it reports a
	// confident 0 for every running session.
	ExitCode *int `json:"exit_code,omitempty"`
}

// Handler is the seam between this package and the rest of lazyshell:
// pkg/gui implements it, and is responsible for whatever goroutine discipline
// each verb needs. Every method may be called concurrently, from a different
// goroutine per connection.
//
// Errors returned here reach the caller as Response.Error, so they are
// user-facing text.
type Handler interface {
	// List reports every session, or only those of group when it is non-empty.
	List(group string) []SessionInfo
	Read(idOrName string, tail int) (string, error)
	New(spec NewSpec) (id string, err error)
	Send(idOrName, text string) error
	Kill(idOrName string) error
	Rename(idOrName, name string) error
	// SetGroup moves a session into group, or out of every group when it is
	// empty.
	SetGroup(idOrName, group string) error
	// GroupSend and GroupKill act on every session of a group and report how
	// many that was. An empty group, or one with no sessions, is an error:
	// silently doing nothing is the worst possible answer to "kill this
	// group", since the caller cannot tell it from success.
	GroupSend(group, text string) (int, error)
	GroupKill(group string) (int, error)
}

// NewSpec is what VerbNew creates, as a struct rather than four positional
// arguments. Same reasoning as session.Options, whose doc comment says it
// outright: this set grows, and each growth would otherwise be a fourth and
// fifth argument on every call site and every fake in the tests.
type NewSpec struct {
	Name    string
	Cwd     string
	Command string
	Group   string
}

// dispatch turns one Request into its Response. Split out from the connection
// loop so the whole verb table is testable without a socket.
func dispatch(h Handler, req Request) Response {
	switch req.Verb {
	case VerbList:
		return Response{OK: true, Sessions: h.List(req.Group)}

	case VerbRead:
		out, err := h.Read(req.ID, req.Tail)
		if err != nil {
			return errorResponse(err)
		}

		return Response{OK: true, Output: out}

	case VerbNew:
		id, err := h.New(NewSpec{Name: req.Name, Cwd: req.Cwd, Command: req.Command, Group: req.Group})
		if err != nil {
			return errorResponse(err)
		}

		return Response{OK: true, ID: id}

	case VerbSend:
		if err := h.Send(req.ID, req.Text); err != nil {
			return errorResponse(err)
		}

		return Response{OK: true}

	case VerbKill:
		if err := h.Kill(req.ID); err != nil {
			return errorResponse(err)
		}

		return Response{OK: true}

	case VerbRename:
		if err := h.Rename(req.ID, req.Name); err != nil {
			return errorResponse(err)
		}

		return Response{OK: true}

	case VerbGroup:
		if err := h.SetGroup(req.ID, req.Group); err != nil {
			return errorResponse(err)
		}

		return Response{OK: true}

	case VerbGroupSend:
		count, err := h.GroupSend(req.Group, req.Text)
		if err != nil {
			return errorResponse(err)
		}

		return Response{OK: true, Count: count}

	case VerbGroupKill:
		count, err := h.GroupKill(req.Group)
		if err != nil {
			return errorResponse(err)
		}

		return Response{OK: true, Count: count}
	}

	return Response{OK: false, Error: fmt.Sprintf("verbe inconnu %q", req.Verb)}
}

func errorResponse(err error) Response {
	return Response{OK: false, Error: err.Error()}
}
