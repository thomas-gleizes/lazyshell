package app

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Sub-commands. They never open the UI: both are things you run once, from a
// shell, about a project file.
const (
	// CommandRun is the default: start the interface.
	CommandRun = ""
	// CommandInit writes a commented lazyshell.yml in the current directory.
	CommandInit = "init"
	// CommandAllow approves a project file without launching anything, the
	// equivalent of `direnv allow`.
	CommandAllow = "allow"
	// CommandConfig writes or inspects the user configuration. Its verb is in
	// Invocation.Arg.
	CommandConfig = "config"
	// CommandHook pushes an authoritative AI agent state (Invocation.Arg) for
	// the calling session — the command an agent's own hook config runs, over
	// $LAZYSHELL_SOCK. See pkg/hook and pkg/app/hook.go.
	CommandHook = "hook"
)

// Verbs of `lazyshell config`. A bare `lazyshell config` means ConfigShow:
// reading is safe and is what someone typing the command blind most likely
// wants, whereas defaulting to init would create a file they did not ask for.
const (
	ConfigShow = "show"
	ConfigInit = "init"
)

// Options are the run-time flags.
type Options struct {
	// ConfigFile is --config-file/-f: an explicit project file, which wins over
	// every other discovery rule.
	ConfigFile string
	// NoAutostart is --no-autostart: open the UI without starting anything the
	// project file declares.
	NoAutostart bool
	// Version is --version: print the build version and exit, without touching
	// the terminal or the config. Not a sub-command, since it must also work as
	// a trailing flag (`lazyshell --version`) the way most CLIs accept it.
	Version bool
	// Agents is `init --agents`: print the AI agent hook config blocks
	// instead of writing lazyshell.yml. Meaningless outside CommandInit, so
	// checked only there — see PrintAgentHookConfig.
	Agents bool
}

// Invocation is a fully parsed command line.
type Invocation struct {
	Options

	// Command is one of the Command* constants.
	Command string
	// Arg is the sub-command's positional argument: the file to approve for
	// `allow`, the verb for `config`, empty for `init`.
	Arg string
}

const usage = `lazyshell — gestionnaire de sessions shell en TUI

Usage :
  lazyshell [options]           ouvre l'interface
  lazyshell init                écrit un lazyshell.yml commenté dans le dossier courant
  lazyshell init --agents       affiche la config de hooks à coller chez ses agents IA
  lazyshell allow [fichier]     approuve un fichier de projet sans rien lancer
  lazyshell config show         affiche la configuration réellement appliquée
  lazyshell config init         écrit une config utilisateur commentée
  lazyshell hook <état>         signale l'état d'un agent IA (idle/working/blocked/done) —
                                 appelée par la config de hook de l'agent, pas à la main

Options :
  -f, --config-file <fichier>   fichier de projet à utiliser
      --no-autostart            n'ouvre que l'interface, ne démarre aucune session déclarée
      --agents                  avec init : affiche la config de hooks au lieu du fichier de projet
      --version                 affiche la version et quitte
`

// ParseArgs turns a command line into an Invocation. It lives here rather than
// in main so it can be tested without a process.
func ParseArgs(args []string) (Invocation, error) {
	var inv Invocation

	// The sub-command, when there is one, must come first — before any flag, so
	// that `lazyshell allow ./x.yml` never has to guess whether ./x.yml belongs
	// to the sub-command or to a preceding flag.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case CommandInit, CommandAllow, CommandConfig, CommandHook:
			inv.Command = args[0]
			args = args[1:]
		default:
			return inv, fmt.Errorf("commande inconnue %q\n\n%s", args[0], usage)
		}
	}

	fs := flag.NewFlagSet("lazyshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	fs.StringVar(&inv.ConfigFile, "config-file", "", "fichier de projet à utiliser")
	fs.StringVar(&inv.ConfigFile, "f", "", "fichier de projet à utiliser (raccourci)")
	fs.BoolVar(&inv.NoAutostart, "no-autostart", false, "ne démarre aucune session déclarée")
	fs.BoolVar(&inv.Version, "version", false, "affiche la version et quitte")
	fs.BoolVar(&inv.Agents, "agents", false, "avec init : affiche la config de hooks des agents IA")

	if err := fs.Parse(args); err != nil {
		return inv, fmt.Errorf("%w\n\n%s", err, usage)
	}

	rest := fs.Args()

	switch {
	case inv.Command == CommandAllow && len(rest) > 0:
		inv.Arg = rest[0]
		rest = rest[1:]

	case inv.Command == CommandHook:
		// Required, unlike allow's optional file: a hook config an agent
		// runs on its own always supplies one ("lazyshell hook working"), so
		// a missing argument here is a human typo, worth the usual usage
		// message — RunHook's own graceful degradation is for an event name
		// that parses as a flag but is not one of the four valid states.
		if len(rest) == 0 {
			return inv, fmt.Errorf("lazyshell hook attend un état (idle, working, blocked ou done)\n\n%s", usage)
		}

		inv.Arg = rest[0]
		rest = rest[1:]

	case inv.Command == CommandConfig:
		// Validated here rather than in the handler: an unknown verb is a typo,
		// and `lazyshell config sho` silently showing nothing (or worse,
		// writing a file) would be the wrong answer to it.
		inv.Arg = ConfigShow

		if len(rest) > 0 {
			if rest[0] != ConfigShow && rest[0] != ConfigInit {
				return inv, fmt.Errorf("commande inconnue %q pour config (attendu : %s ou %s)\n\n%s",
					rest[0], ConfigShow, ConfigInit, usage)
			}

			inv.Arg = rest[0]
			rest = rest[1:]
		}
	}

	if len(rest) > 0 {
		return inv, fmt.Errorf("argument inattendu %q\n\n%s", rest[0], usage)
	}

	return inv, nil
}
