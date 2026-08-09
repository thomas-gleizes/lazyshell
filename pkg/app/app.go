// Package app wires the pieces of lazyshell together: configuration, the
// session manager and the GUI. It owns the bootstrap sequence and nothing else.
package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/debug"
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
	"github.com/thomas-gleizes/lazyshell/pkg/version"
)

// App is the top-level object of lazyshell.
type App struct {
	sessions *session.Manager
	gui      *gui.Gui
	// debug is --debug's recorder, nil when the flag was not given. Every
	// method of *debug.Logger is nil-safe, so nothing downstream tests it.
	debug *debug.Logger
}

// Main is the whole command: parse the arguments, then either run a
// sub-command or open the interface. cmd/lazyshell is nothing but a call to
// this.
func Main(args []string, out io.Writer) error {
	inv, err := ParseArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(out, usage)

			return nil
		}

		return err
	}

	if inv.Version {
		fmt.Fprintf(out, "lazyshell %s\n", version.Version)

		return nil
	}

	switch inv.Command {
	case CommandInit:
		if inv.Agents {
			return PrintAgentHookConfig(out)
		}

		return InitProject(".", out)
	case CommandAllow:
		return AllowProject(inv.Arg, out)
	case CommandConfig:
		if inv.Arg == ConfigInit {
			return InitUserConfig(config.Path(), out)
		}

		return ShowConfig(inv.Options, out, os.Stderr)
	case CommandHook:
		return RunHook(inv.Arg, os.Stderr)
	case CommandCtl:
		return RunCtl(inv, out)
	}

	return New(inv.Options).Run()
}

// New builds the application without touching the terminal: it loads the
// configuration, discovers and (with the user's approval) starts the sessions a
// project file declares. A malformed config file is reported on stderr and
// falls back to defaults rather than preventing lazyshell from starting — the
// terminal is not yet taken over at this point, so it is still safe to print
// directly, and to prompt on stdin.
func New(opts Options) *App {
	return newApp(opts, stdinApprover(), os.Stderr)
}

// newApp is New with its outside world injected, so the bootstrap — including
// the approval prompt — is testable without a terminal.
func newApp(opts Options, approve approver, errOut io.Writer) *App {
	cfg, err := config.Load(config.Path())
	if err != nil {
		fmt.Fprintf(errOut, "lazyshell: %v (using defaults)\n", err)

		cfg = config.Default()
	}

	startupErrs := checkConfig(&cfg, errOut)

	dbg, debugErr := openDebugLog(opts)
	if debugErr != nil {
		startupErrs = append(startupErrs, debugErr)
	}

	cfg, pcfg, projectErrs := loadProject(opts, cfg, errOut)
	startupErrs = append(startupErrs, projectErrs...)

	sessions := session.NewManager()
	sessions.ScrollbackSize = cfg.ScrollbackSize
	sessions.Term = cfg.Term
	sessions.DefaultEnvFiles = opts.EnvFiles
	sessions.DisableDefaultEnv = opts.NoEnvFile

	// Only when the control API is on: an empty ControlSocket is what keeps
	// $LAZYSHELL_CONTROL_SOCK out of every session's environment, which is how
	// a session learns the feature is off (pkg/session's buildEnv, pkg/control).
	if cfg.Control.Enabled {
		sessions.ControlSocket = config.ControlSocketPath()
	}

	if cfg.KillTimeoutMs > 0 {
		sessions.KillTimeout = time.Duration(cfg.KillTimeoutMs) * time.Millisecond
	}

	detector, warnings := agent.LoadManifests(config.AgentsDir())
	sessions.Detector = detector

	for _, w := range warnings {
		fmt.Fprintf(errOut, "lazyshell: %v\n", w)
	}

	switch {
	case len(pcfg.Sessions) == 0:
		// Nothing declared (or no project file at all): start a single
		// default session so the user isn't dropped into an empty list —
		// but leave focus on the sessions panel (gui.New / layout default),
		// not inside the new shell.
		if _, err := sessions.New("session-1", resolveShell(cfg.Shell)); err != nil {
			startupErrs = append(startupErrs, err)
		}
	case opts.NoAutostart:
		startupErrs = append(startupErrs,
			fmt.Errorf("--no-autostart : %d session(s) déclarée(s) non démarrée(s)", len(pcfg.Sessions)))
	case !approve.approve(pcfg):
		startupErrs = append(startupErrs,
			fmt.Errorf("%s non approuvé : rien n'a été démarré (lazyshell allow)", pcfg.Path))
	default:
		startupErrs = append(startupErrs, autostart(sessions, pcfg, resolveShell(cfg.Shell))...)
	}

	// The declared group order, whether or not any session was started: it is
	// a display order, and a session assigned to a declared group at runtime
	// must land where the file said it would. Bad entries are reported like
	// every other project-file problem and the good ones still apply.
	groups, groupErrs := pcfg.ResolvedGroups()
	startupErrs = append(startupErrs, groupErrs...)

	g := gui.New(sessions, cfg)
	g.SetGroupOrder(groups)
	g.SetDebug(dbg)
	g.SetStartupError(joinErrors(startupErrs))

	return &App{sessions: sessions, gui: g, debug: dbg}
}

// openDebugLog honours --debug. A log file that cannot be opened is reported
// like every other bootstrap problem and lazyshell starts anyway, without the
// debug mode: an unwritable ~/.config must not be the reason a session manager
// refuses to run.
func openDebugLog(opts Options) (*debug.Logger, error) {
	if !opts.Debug {
		return nil, nil
	}

	dbg, err := debug.New(config.DebugLogPath())
	if err != nil {
		return nil, fmt.Errorf("--debug : %w", err)
	}

	dbg.Event("lazyshell %s started — debug log at %s", version.Version, dbg.Path())

	return dbg, nil
}

// checkConfig runs every validation the user configuration is subject to, in
// the one window where saying something is still possible: gocui has not taken
// the terminal yet, so warnings printed here are readable, and errors returned
// here reach the status bar through SetStartupError.
//
// Nothing it finds is fatal. Config.Validate has already replaced out-of-range
// values with their defaults, and the key/color checks report values that their
// consumers were already falling back on — silently, which is the actual bug
// this fixes. A config file typo must never be the reason a session manager
// refuses to start.
//
// Unknown keys go to stderr rather than to the status bar: they are the loudest
// and least urgent of the three, and the status bar is one line.
func checkConfig(cfg *config.Config, errOut io.Writer) []error {
	for _, w := range cfg.Warnings {
		fmt.Fprintf(errOut, "lazyshell: %s: clé ignorée dans la config utilisateur (%s)\n", config.Path(), w)
	}

	errs := cfg.Validate()
	errs = append(errs, gui.ValidateConfig(*cfg)...)

	for _, err := range errs {
		fmt.Fprintf(errOut, "lazyshell: config: %v\n", err)
	}

	return errs
}

// loadProject discovers and reads the project file, merges it onto the user
// configuration and reports its ignored keys. A project file that cannot be
// read or parsed is an error the user sees, not a reason to refuse to start.
func loadProject(opts Options, cfg config.Config, errOut io.Writer) (config.Config, config.ProjectConfig, []error) {
	path := config.ProjectPath(opts.ConfigFile)
	if path == "" {
		return cfg, config.ProjectConfig{}, nil
	}

	pcfg, err := config.LoadProject(path)
	if err != nil {
		return cfg, config.ProjectConfig{}, []error{err}
	}

	for _, w := range pcfg.Warnings {
		fmt.Fprintf(errOut, "lazyshell: %s: clé ignorée dans un fichier de projet (%s)\n", pcfg.Path, w)
	}

	return cfg.MergeProject(pcfg), pcfg, nil
}

// resolveShell mirrors pkg/gui's defaultShell for the sessions started before
// the GUI exists: the configured shell, else $SHELL, else /bin/bash.
func resolveShell(configured string) string {
	if configured != "" {
		return configured
	}

	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	return "/bin/bash"
}

// joinErrors folds the startup problems into the single line the status bar
// has room for.
func joinErrors(errs []error) string {
	if len(errs) == 0 {
		return ""
	}

	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}

	return strings.Join(msgs, " · ")
}

// Run starts the GUI and blocks until the user quits. The terminal is
// restored before it returns, whatever the outcome. Every session is killed
// before Run returns: there is no detach in the MVP, everything dies with
// lazyshell.
func (a *App) Run() error {
	// Registered before Shutdown so LIFO runs it last: anything the teardown
	// logs still lands in the file. No-op when --debug was not given.
	defer func() { _ = a.debug.Close() }()
	defer a.sessions.Shutdown()

	return a.gui.Run()
}
