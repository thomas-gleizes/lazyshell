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
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
	"github.com/thomas-gleizes/lazyshell/pkg/version"
)

// App is the top-level object of lazyshell.
type App struct {
	sessions *session.Manager
	gui      *gui.Gui
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
		return InitProject(".", out)
	case CommandAllow:
		return AllowProject(inv.Arg, out)
	case CommandConfig:
		if inv.Arg == ConfigInit {
			return InitUserConfig(config.Path(), out)
		}

		return ShowConfig(inv.Options, out, os.Stderr)
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

	cfg, pcfg, projectErrs := loadProject(opts, cfg, errOut)
	startupErrs = append(startupErrs, projectErrs...)

	sessions := session.NewManager()
	sessions.ScrollbackSize = cfg.ScrollbackSize
	sessions.Term = cfg.Term

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
		// Nothing declared (or no project file at all): the pre-phase-6
		// behaviour, an empty list waiting for "n".
	case opts.NoAutostart:
		startupErrs = append(startupErrs,
			fmt.Errorf("--no-autostart : %d session(s) déclarée(s) non démarrée(s)", len(pcfg.Sessions)))
	case !approve.approve(pcfg):
		startupErrs = append(startupErrs,
			fmt.Errorf("%s non approuvé : rien n'a été démarré (lazyshell allow)", pcfg.Path))
	default:
		startupErrs = append(startupErrs, autostart(sessions, pcfg, resolveShell(cfg.Shell))...)
	}

	g := gui.New(sessions, cfg)
	g.SetStartupError(joinErrors(startupErrs))

	return &App{sessions: sessions, gui: g}
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
	defer a.sessions.Shutdown()

	return a.gui.Run()
}
