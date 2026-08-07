package app

import (
	"fmt"
	"io"
	"os"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/hook"
)

// RunHook implements `lazyshell hook <event>` — the command a session's own
// agent hook config runs to push an authoritative state (see pkg/hook and
// pkg/session's SetAgentState). It always returns nil: this command's whole
// purpose is to run as a side effect of the agent's own hook mechanism, and
// a non-zero exit here would make lazyshell the reason an otherwise-healthy
// agent turn fails. Every failure mode — no $LAZYSHELL_SOCK (not running
// under lazyshell), an event name that is not one of the four states, a
// socket that has gone away — degrades to a diagnostic on errOut and
// nothing else.
func RunHook(event string, errOut io.Writer) error {
	state, ok := agent.ParseState(event)
	if !ok {
		fmt.Fprintf(errOut, "lazyshell hook: état inconnu %q (attendu : idle, working, blocked ou done)\n", event)

		return nil
	}

	sockPath := os.Getenv("LAZYSHELL_SOCK")
	if sockPath == "" {
		fmt.Fprintln(errOut, "lazyshell hook: $LAZYSHELL_SOCK n'est pas défini — cette session ne tourne pas sous lazyshell")

		return nil
	}

	if err := hook.Send(sockPath, state); err != nil {
		fmt.Fprintf(errOut, "lazyshell hook: %v\n", err)
	}

	return nil
}
