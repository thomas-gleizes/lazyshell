// Package agent detects, without any user configuration, whether a process
// running inside a session is an AI coding agent CLI (claude, codex,
// opencode, ...) and what it is currently doing: idle, working, or waiting
// on the user. It never talks to the agent — only to the session's terminal
// emulator (pkg/screen) — so a detector that gets it wrong degrades a gutter
// marker, never the session itself.
package agent

// State is the four-value classification herdr popularized, and that this
// package's design report (RAPPORT_ANALYSE_INTEGRATION_AGENTS_IA.md) settled
// on: it is the split tmux does not make, between "produced output" and "is
// waiting for you".
type State int

const (
	// StateNone means the session's foreground process does not match any
	// known agent manifest — it is not an agent session at all, and the
	// gutter must not show a marker for it. Distinct from StateIdle, which
	// means "this is an agent, and it is not doing anything right now".
	StateNone State = iota
	StateIdle
	StateWorking
	StateBlocked
	StateDone
)

// String is used by manifest validation errors and tests; it is not meant
// for the gutter, which renders a configurable glyph instead.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateWorking:
		return "working"
	case StateBlocked:
		return "blocked"
	case StateDone:
		return "done"
	default:
		return "none"
	}
}

// parseState is manifest.go's counterpart to String, used when decoding a
// rule's "state:" key.
func parseState(s string) (State, bool) {
	switch s {
	case "idle":
		return StateIdle, true
	case "working":
		return StateWorking, true
	case "blocked":
		return StateBlocked, true
	case "done":
		return StateDone, true
	default:
		return StateNone, false
	}
}
