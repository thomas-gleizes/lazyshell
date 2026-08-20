package app

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/thomas-gleizes/lazyshell/pkg/control"
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
	// CommandUpdate replaces the running binary with the latest GitHub
	// release — the in-binary equivalent of scripts/install.sh. See
	// pkg/update and pkg/app/update_cmd.go.
	CommandUpdate = "update"
	// CommandCtl drives a running lazyshell over the agent control socket:
	// list/read/new/send/kill/rename/wait plus the group verbs, in
	// Invocation.CtlVerb. Only works when
	// config.Control.Enabled is true — see pkg/control and pkg/app/ctl.go.
	CommandCtl = "ctl"
)

// Verbs of `lazyshell ctl`, mirroring pkg/control's own constants. Repeated
// here rather than imported so the parser reports a typo without pkg/control
// being reachable, and so this list is what the usage text is checked against.
var ctlVerbs = map[string]struct{ minArgs, maxArgs int }{
	control.VerbList:   {0, 0},
	control.VerbRead:   {1, 1},
	control.VerbNew:    {0, 0},
	control.VerbSend:   {2, -1}, // target + text, the text possibly several words
	control.VerbKill:   {1, 1},
	control.VerbRename: {2, 2},

	control.VerbGroup:     {2, 2}, // session + group
	control.VerbGroupSend: {2, -1},
	control.VerbGroupKill: {1, 1},

	// 0 or 1 positional: the session, when --group is not used instead — see
	// the VerbWait-specific check in parseCtlArgs, which arity alone cannot
	// express (it must be exactly one of {positional, --group}, not either).
	control.VerbWait: {0, 1},

	// A client-side alias only: it maps to VerbGroup with an empty group.
	// Spelled out as its own verb because "lazyshell ctl group api" with the
	// group left off would be a plausible typo, not a request to ungroup.
	ctlVerbUngroup: {1, 1},
}

// ctlVerbUngroup is the CLI's name for "VerbGroup with no group". It has no
// pkg/control counterpart on purpose — the wire protocol needs no second verb
// to express an empty string.
const ctlVerbUngroup = "ungroup"

// Verbs of `lazyshell config`. A bare `lazyshell config` means ConfigShow:
// reading is safe and is what someone typing the command blind most likely
// wants, whereas defaulting to init would create a file they did not ask for.
const (
	ConfigShow = "show"
	ConfigInit = "init"
	// ConfigEdit opens the user configuration in $VISUAL/$EDITOR (falling back
	// to nano/vim/vi) and reports what lazyshell makes of the saved file.
	ConfigEdit = "edit"
)

// configVerbs is what `lazyshell config` accepts, in the order the usage text
// and the error message list them.
var configVerbs = []string{ConfigShow, ConfigInit, ConfigEdit}

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
	// EnvFiles is one or more --env-file flags, in the order given: extra
	// .env-style files loaded for every session this run creates, after each
	// session's own default "<cwd>/.env" — see session.Manager.DefaultEnvFiles.
	EnvFiles []string
	// NoEnvFile is --no-env-file: skip the automatic "<cwd>/.env" lookup
	// entirely, for every session this run creates.
	NoEnvFile bool
	// Check is `update --check`: report what the latest release is and stop,
	// without downloading or replacing anything. Meaningless outside
	// CommandUpdate, so checked only there — same as Agents above.
	Check bool
	// Force is `update --force`: install the latest release even when the
	// installed version is not older than it, or is not a published version at
	// all (a binary built from source).
	Force bool
	// Debug is --debug: record every keystroke, action and lifecycle event to
	// config.DebugLogPath() and show the last of them in a floating panel over
	// the output panel (F12 toggles it). Off by default and deliberately not a
	// config key: it is a "I am chasing something right now" switch, not a
	// preference, and the log holds everything typed into a shell.
	Debug bool
}

// envFileFlag collects every occurrence of the repeatable --env-file flag, in
// the order given on the command line — later files are meant to override
// earlier ones, so the order must survive parsing.
type envFileFlag struct{ values *[]string }

func (f envFileFlag) String() string { return "" }

func (f envFileFlag) Set(v string) error {
	*f.values = append(*f.values, v)

	return nil
}

// CtlOptions are `lazyshell ctl`'s own flags. Kept apart from Options, which
// is about the interface this command never opens.
type CtlOptions struct {
	// Name is `ctl new --name` (the session to create) and is unused by
	// `ctl rename`, whose new name is a positional argument.
	Name string
	// Cwd is `ctl new --cwd`: the working directory to start in.
	Cwd string
	// Command is `ctl new --command`: typed into the new session's shell.
	Command string
	// Group is `ctl new --group` (the group to create the session in) and
	// `ctl list --group` (show only that group). The group verbs take theirs
	// as a positional argument instead, since there it is the target rather
	// than a modifier.
	Group string
	// Tail is `ctl read --tail`: last N lines only. Zero is the whole
	// scrollback.
	Tail int
	// Enter is `ctl send --enter`: append a carriage return to the text.
	// Off by default — pressing Enter is an explicit act, same as tmux's
	// send-keys.
	Enter bool
	// JSON prints the raw control.Response instead of the human rendering,
	// for a caller that would rather parse than scrape.
	JSON bool
	// State is `ctl wait --state`: the agent state to wait for (idle/working/
	// blocked/done).
	State string
	// Timeout is `ctl wait --timeout`, in seconds. Zero or unset means
	// control.DefaultWaitTimeout.
	Timeout int
}

// Invocation is a fully parsed command line.
type Invocation struct {
	Options

	// Command is one of the Command* constants.
	Command string
	// Arg is the sub-command's positional argument: the file to approve for
	// `allow`, the verb for `config`, empty for `init`.
	Arg string
	// CtlVerb is `ctl`'s verb, one of pkg/control's Verb* constants, already
	// checked against ctlVerbs.
	CtlVerb string
	// CtlArgs are the verb's positional arguments, count already checked.
	CtlArgs []string
	// Ctl are `ctl`'s flags.
	Ctl CtlOptions
}

const usage = `lazyshell — gestionnaire de sessions shell en TUI

Usage :
  lazyshell [options]           ouvre l'interface
  lazyshell init                écrit un lazyshell.yml commenté dans le dossier courant
  lazyshell init --agents       affiche la config de hooks à coller chez ses agents IA
  lazyshell allow [fichier]     approuve un fichier de projet sans rien lancer
  lazyshell config show         affiche la configuration réellement appliquée
  lazyshell config init         écrit une config utilisateur commentée
  lazyshell config edit         ouvre la config utilisateur dans $EDITOR (à défaut nano/vim/vi),
                                 la crée si elle n'existe pas, et vérifie le fichier enregistré
  lazyshell hook <état>         signale l'état d'un agent IA (idle/working/blocked/done) —
                                 appelée par la config de hook de l'agent, pas à la main
  lazyshell update              remplace le binaire installé par la dernière release
  lazyshell update --check      dit s'il y a une mise à jour, sans rien installer

Pilotage d'un lazyshell en cours (nécessite control.enabled dans la config) :
  lazyshell ctl list [--group G]            liste les sessions
  lazyshell ctl read <session> [--tail N]   affiche la sortie d'une session
  lazyshell ctl new [--name N] [--cwd D] [--command C] [--group G]
                                            crée une session
  lazyshell ctl send <session> <texte…> [--enter]
                                            écrit dans une session, comme au clavier
  lazyshell ctl kill <session>              termine une session
  lazyshell ctl rename <session> <nom>      renomme une session
  lazyshell ctl group <session> <groupe>    affecte une session à un groupe
  lazyshell ctl ungroup <session>           retire la session de son groupe
  lazyshell ctl group-send <groupe> <texte…> [--enter]
                                            écrit dans tout un groupe
  lazyshell ctl group-kill <groupe>         termine tout un groupe
  lazyshell ctl wait <session> --state E [--timeout N]
                                            attend qu'une session atteigne l'état E
  lazyshell ctl wait --group G --state E [--timeout N]
                                            attend que le premier membre du groupe l'atteigne

  <session> est un id (session-2) ou un nom exact ; --json donne la réponse brute.
  --command est tapé dans le shell de la session, il ne le remplace pas : une
    commande bloquante (sleep 300) rend la session sourde aux send suivants,
    qui iront dans son entrée standard et non dans le shell.
  send tape le texte sans l'exécuter ; --enter ajoute le retour chariot.
  kill termine le processus mais laisse la session listée (statut exited, avec
    son code de sortie) : la retirer pour de bon reste la touche D, dans l'interface.
  Les verbes de groupe agissent sur toutes les sessions du groupe et affichent
    combien ont été touchées ; un groupe vide ou inconnu est une erreur, pas un
    « 0 session » silencieux. group-send saute les sessions déjà terminées.
  Relancer un groupe n'est pas exposé ici : aucun verbe restart n'existe pour
    une session seule non plus. C'est la touche W, dans l'interface.
  wait bloque jusqu'à l'état demandé (idle/working/blocked/done) ou --timeout
    (120 s par défaut) ; sortie non nulle si le délai expire ou si la session
    visée se termine avant, comme tout échec de ctl.

Options :
  -f, --config-file <fichier>   fichier de projet à utiliser
      --no-autostart            n'ouvre que l'interface, ne démarre aucune session déclarée
      --env-file <fichier>      charge un fichier .env supplémentaire (répétable, le dernier gagne)
      --no-env-file             ne charge pas automatiquement le .env du dossier courant
      --agents                  avec init : affiche la config de hooks au lieu du fichier de projet
      --check                   avec update : ne fait que signaler une mise à jour disponible
      --force                   avec update : installe la dernière release même si rien de plus
                                 récent n'est disponible, ou si le binaire vient d'une compilation
                                 locale
      --debug                   journalise touches, actions et évènements et affiche un panneau
                                 de debug (F12 pour le masquer)
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
		case CommandInit, CommandAllow, CommandConfig, CommandHook, CommandCtl, CommandUpdate:
			inv.Command = args[0]
			args = args[1:]
		default:
			return inv, fmt.Errorf("commande inconnue %q\n\n%s", args[0], usage)
		}
	}

	// ctl parses on its own: none of the flags below mean anything to it, and
	// its arguments are interspersed with its own flags ("ctl send s1 echo hi
	// --enter"), which the single Parse used here cannot do.
	if inv.Command == CommandCtl {
		return parseCtlArgs(inv, args)
	}

	fs := flag.NewFlagSet("lazyshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	fs.StringVar(&inv.ConfigFile, "config-file", "", "fichier de projet à utiliser")
	fs.StringVar(&inv.ConfigFile, "f", "", "fichier de projet à utiliser (raccourci)")
	fs.BoolVar(&inv.NoAutostart, "no-autostart", false, "ne démarre aucune session déclarée")
	fs.Var(envFileFlag{&inv.EnvFiles}, "env-file", "fichier .env supplémentaire à charger (répétable)")
	fs.BoolVar(&inv.NoEnvFile, "no-env-file", false, "ne charge pas automatiquement le .env du dossier courant")
	fs.BoolVar(&inv.Version, "version", false, "affiche la version et quitte")
	fs.BoolVar(&inv.Agents, "agents", false, "avec init : affiche la config de hooks des agents IA")
	fs.BoolVar(&inv.Check, "check", false, "avec update : indique s'il y a une mise à jour sans l'installer")
	fs.BoolVar(&inv.Force, "force", false, "avec update : installe même si rien de plus récent n'est disponible")
	fs.BoolVar(&inv.Debug, "debug", false, "journalise touches et actions dans un fichier et un panneau")

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
			if !slices.Contains(configVerbs, rest[0]) {
				return inv, fmt.Errorf("commande inconnue %q pour config (attendu : %s)\n\n%s",
					rest[0], strings.Join(configVerbs, ", "), usage)
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

// parseCtlArgs parses `lazyshell ctl`'s tail: a verb, its positional
// arguments, and its flags, in any order.
//
// The repeated-Parse loop is the standard Go idiom for interspersed flags and
// arguments — flag.Parse stops at the first non-flag — and it is needed here
// rather than anywhere else because the natural way to write these commands
// puts a positional first: `ctl send session-1 "echo bonjour" --enter`.
func parseCtlArgs(inv Invocation, args []string) (Invocation, error) {
	fs := flag.NewFlagSet("lazyshell ctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	fs.StringVar(&inv.Ctl.Name, "name", "", "nom de la session à créer")
	fs.StringVar(&inv.Ctl.Cwd, "cwd", "", "dossier de travail de la session à créer")
	fs.StringVar(&inv.Ctl.Command, "command", "", "commande tapée dans la session créée")
	fs.StringVar(&inv.Ctl.Group, "group", "", "groupe de la session créée, ou filtre pour list")
	fs.IntVar(&inv.Ctl.Tail, "tail", 0, "n'affiche que les N dernières lignes")
	fs.BoolVar(&inv.Ctl.Enter, "enter", false, "ajoute un retour chariot au texte envoyé")
	fs.BoolVar(&inv.Ctl.JSON, "json", false, "affiche la réponse brute en JSON")
	fs.StringVar(&inv.Ctl.State, "state", "", "état d'agent à attendre (idle, working, blocked, done)")
	fs.IntVar(&inv.Ctl.Timeout, "timeout", 0, "délai maximum en secondes pour wait (0 = défaut)")

	var positional []string

	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return inv, fmt.Errorf("%w\n\n%s", err, usage)
		}

		if fs.NArg() == 0 {
			break
		}

		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}

	if len(positional) == 0 {
		return inv, fmt.Errorf("lazyshell ctl attend un verbe (%s)\n\n%s", ctlVerbList(), usage)
	}

	inv.CtlVerb = positional[0]
	inv.CtlArgs = positional[1:]

	arity, ok := ctlVerbs[inv.CtlVerb]
	if !ok {
		return inv, fmt.Errorf("verbe inconnu %q pour ctl (attendu : %s)\n\n%s",
			inv.CtlVerb, ctlVerbList(), usage)
	}

	// Checked here rather than in RunCtl for the reason `config`'s verb is:
	// a wrong argument count is a typo, and the answer to a typo is the usage
	// message, not a request sent to a running lazyshell.
	if got := len(inv.CtlArgs); got < arity.minArgs || (arity.maxArgs >= 0 && got > arity.maxArgs) {
		return inv, fmt.Errorf("lazyshell ctl %s : %d argument(s), %s attendu(s)\n\n%s",
			inv.CtlVerb, got, arityText(arity.minArgs, arity.maxArgs), usage)
	}

	// wait's real arity — exactly one of {positional session, --group}, never
	// both and never neither — is not expressible by ctlVerbs' {min,max}
	// pair alone, so it is checked here instead, on the same "a typo is a
	// usage error, not a request sent to a running lazyshell" philosophy.
	if inv.CtlVerb == control.VerbWait {
		if inv.Ctl.State == "" {
			return inv, fmt.Errorf("lazyshell ctl wait attend --state (idle, working, blocked ou done)\n\n%s", usage)
		}

		switch {
		case len(inv.CtlArgs) == 0 && inv.Ctl.Group == "":
			return inv, fmt.Errorf("lazyshell ctl wait attend une session ou --group\n\n%s", usage)
		case len(inv.CtlArgs) == 1 && inv.Ctl.Group != "":
			return inv, fmt.Errorf("lazyshell ctl wait : préciser une session ou --group, pas les deux\n\n%s", usage)
		}
	}

	return inv, nil
}

func arityText(minArgs, maxArgs int) string {
	if maxArgs < 0 {
		return fmt.Sprintf("au moins %d", minArgs)
	}

	if minArgs == maxArgs {
		return strconv.Itoa(minArgs)
	}

	return fmt.Sprintf("de %d à %d", minArgs, maxArgs)
}

// ctlVerbList is the verbs in a stable order, for error messages — ctlVerbs is
// a map, and an error whose wording changes between runs is a bad error.
func ctlVerbList() string {
	return strings.Join([]string{
		control.VerbList, control.VerbRead, control.VerbNew,
		control.VerbSend, control.VerbKill, control.VerbRename,
		control.VerbWait,
	}, ", ")
}
