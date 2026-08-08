package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/control"
)

// RunCtl implements `lazyshell ctl <verbe>` — the agent control API's client
// (pkg/control). It is the deliberate opposite of RunHook in one respect:
// RunHook always returns nil, because a hook runs as a side effect of an
// agent's turn and lazyshell must never be the reason that turn fails. A ctl
// call *is* the agent's action. An agent that asked for a session and did not
// get one has to find out, so every failure here — no socket, a refused verb,
// an unknown session — is a non-zero exit.
//
// Hence no errOut parameter, unlike RunHook: there is no failure this command
// reports and then shrugs off, so every one of them is the returned error and
// pkg/app's caller prints it.
func RunCtl(inv Invocation, out io.Writer) error {
	path, err := controlSocketPath()
	if err != nil {
		return err
	}

	req, err := buildCtlRequest(inv)
	if err != nil {
		return err
	}

	resp, err := control.Call(path, req)
	if err != nil {
		return fmt.Errorf("lazyshell ctl : %w", err)
	}

	if inv.Ctl.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")

		if err := enc.Encode(resp); err != nil {
			return err
		}

		// Still an error exit when the verb was refused: --json changes the
		// rendering, not whether the command succeeded.
		if !resp.OK {
			return fmt.Errorf("lazyshell ctl %s : %s", inv.CtlVerb, resp.Error)
		}

		return nil
	}

	if !resp.OK {
		return fmt.Errorf("lazyshell ctl %s : %s", inv.CtlVerb, resp.Error)
	}

	printCtlResponse(inv, resp, out)

	return nil
}

// controlSocketPath is where ctl dials.
//
// Inside a lazyshell session it is simply $LAZYSHELL_CONTROL_SOCK. That
// variable being unset *inside a session* is how the feature announces it is
// off (pkg/session's buildEnv injects nothing when it is), so that case gets
// the message that actually helps rather than a missing-file error.
//
// Outside a session — ctl typed into an ordinary terminal — there is nothing to
// inherit, and config.ControlSocketPath() would be useless: it embeds *this*
// process's pid, which never matches a running lazyshell. So the sockets under
// config.RuntimeRoot are probed instead and the live one is used. Exactly one
// is the ordinary case; several running instances is genuinely ambiguous and is
// reported as such rather than guessed at.
func controlSocketPath() (string, error) {
	if p := os.Getenv("LAZYSHELL_CONTROL_SOCK"); p != "" {
		return p, nil
	}

	if os.Getenv("LAZYSHELL_SESSION_ID") != "" {
		return "", fmt.Errorf("lazyshell ctl : l'API de contrôle est désactivée — " +
			"mettre control.enabled à true dans la configuration, puis relancer lazyshell")
	}

	live := liveControlSockets()

	switch len(live) {
	case 1:
		return live[0], nil

	case 0:
		return "", fmt.Errorf("lazyshell ctl : aucun lazyshell joignable — " +
			"soit aucun ne tourne, soit control.enabled n'y est pas activé")

	default:
		return "", fmt.Errorf("lazyshell ctl : %d lazyshell joignables (%s) — "+
			"lancer la commande depuis une session, ou définir $LAZYSHELL_CONTROL_SOCK",
			len(live), strings.Join(live, ", "))
	}
}

// liveControlSockets are the control sockets on this machine something is
// actually listening on. A socket file left behind by a crashed lazyshell
// survives its process, so the file existing is not the question — dialling is
// (control.IsLive).
func liveControlSockets() []string {
	root := config.RuntimeRoot()
	if root == "" {
		return nil
	}

	candidates, err := filepath.Glob(filepath.Join(root, "*", "control.sock"))
	if err != nil {
		return nil
	}

	live := make([]string, 0, len(candidates))

	for _, path := range candidates {
		if control.IsLive(path) {
			live = append(live, path)
		}
	}

	return live
}

// buildCtlRequest maps the parsed command line onto the wire. ParseArgs has
// already checked the verb and its argument count, so this only has to place
// them.
func buildCtlRequest(inv Invocation) (control.Request, error) {
	req := control.Request{Verb: inv.CtlVerb}

	switch inv.CtlVerb {
	case control.VerbList:
	case control.VerbRead:
		req.ID = inv.CtlArgs[0]
		req.Tail = inv.Ctl.Tail

	case control.VerbNew:
		req.Name = inv.Ctl.Name
		req.Cwd = inv.Ctl.Cwd
		req.Command = inv.Ctl.Command

	case control.VerbSend:
		req.ID = inv.CtlArgs[0]
		// Several words stay several words: `ctl send s1 echo bonjour` is the
		// shape a shell hands over when the text was not quoted, and joining
		// is the only reading of it that is not surprising.
		req.Text = strings.Join(inv.CtlArgs[1:], " ")

		if inv.Ctl.Enter {
			req.Text += "\r"
		}

	case control.VerbKill:
		req.ID = inv.CtlArgs[0]

	case control.VerbRename:
		req.ID = inv.CtlArgs[0]
		req.Name = inv.CtlArgs[1]

	default:
		return req, fmt.Errorf("lazyshell ctl : verbe non géré %q", inv.CtlVerb)
	}

	return req, nil
}

// printCtlResponse is the human rendering, used unless --json. Read's output
// goes out raw — it is a terminal's worth of text, and anything wrapped around
// it would be in the way of the `ctl read | grep` this verb exists for.
func printCtlResponse(inv Invocation, resp control.Response, out io.Writer) {
	switch inv.CtlVerb {
	case control.VerbList:
		for _, s := range resp.Sessions {
			line := fmt.Sprintf("%-12s %-20s %s", s.ID, s.Name, s.Status)
			if s.AgentState != "" {
				line += " " + s.AgentState
			}

			fmt.Fprintln(out, line)
		}

	case control.VerbRead:
		fmt.Fprint(out, resp.Output)

	case control.VerbNew:
		fmt.Fprintln(out, resp.ID)
	}
}
