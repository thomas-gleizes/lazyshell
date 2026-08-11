package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// approver asks the user whether a project file may be executed. It is a
// struct rather than a bare function so tests can drive the prompt without a
// terminal — which is also exactly what a non-interactive run (CI, a piped
// stdin) needs.
type approver struct {
	in  io.Reader
	out io.Writer
	// interactive is false when stdin is not a terminal. Approval is then
	// refused outright instead of read from whatever happens to be piped in:
	// failing closed is the only safe default for "run these commands?".
	interactive bool
}

// stdinApprover is the production approver: prompts on stderr, reads stdin.
// Everything it does happens before gocui takes over the terminal, which is
// what makes writing and reading here safe.
func stdinApprover() approver {
	return approver{in: os.Stdin, out: os.Stderr, interactive: isTerminal(os.Stdin)}
}

// isTerminal must be an actual terminal test (a TCGETS ioctl), not a
// Stat/ModeCharDevice one: /dev/null is a character device too, so the cheap
// check reports `lazyshell < /dev/null` as interactive and prompts into a void
// that can never answer.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// approve shows the project file and asks for a yes/no. On yes, the approval is
// recorded so it is not asked again until the file changes.
func (a approver) approve(pcfg config.ProjectConfig) bool {
	if config.IsTrusted(pcfg.Path, pcfg.Raw) {
		return true
	}

	if !a.interactive {
		return false
	}

	fmt.Fprintf(a.out, "\nlazyshell : %s n'a jamais été approuvé.\n", pcfg.Path)
	fmt.Fprintf(a.out, "Il va démarrer %d session(s) et exécuter leurs commandes. Contenu :\n\n", len(pcfg.Sessions))

	for line := range strings.SplitSeq(strings.TrimRight(string(pcfg.Raw), "\n"), "\n") {
		fmt.Fprintf(a.out, "  │ %s\n", line)
	}

	fmt.Fprint(a.out, "\nApprouver et mémoriser ? [y/N] ")

	answer, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && answer == "" {
		return false
	}

	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Fprint(a.out, "Refusé : aucune session ne sera démarrée.\n")

		return false
	}

	if err := config.Trust(pcfg.Path, pcfg.Raw); err != nil {
		// Worth starting anyway — the user just said yes — but they need to know
		// the answer was not remembered.
		fmt.Fprintf(a.out, "lazyshell : approbation non mémorisée : %v\n", err)
	}

	return true
}

// autostart creates one session per valid entry of the project file, in file
// order. Errors are collected rather than returned: a single bad entry must
// not cost the user the sessions that are fine, so every problem ends up in the
// status bar instead of aborting the launch.
func autostart(sessions *session.Manager, pcfg config.ProjectConfig, shell string) []error {
	specs, errs := pcfg.Validate()

	for _, spec := range specs {
		watch := make([]session.WatchSpec, len(spec.Watch))
		for i, w := range spec.Watch {
			watch[i] = session.WatchSpec{Pattern: w.Pattern, Notify: w.Notify}
		}

		if _, err := sessions.NewWithOptions(session.Options{
			Name:             spec.Name,
			Group:            spec.Group,
			Shell:            shell,
			Cwd:              spec.Cwd,
			Env:              spec.Env,
			EnvFiles:         spec.EnvFiles,
			NoDefaultEnvFile: spec.NoDefaultEnv,
			Command:          spec.Command,
			Watch:            watch,
		}); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// AllowProject implements `lazyshell allow`: approve a project file without
// launching anything. An empty path means "whatever this directory resolves to".
func AllowProject(path string, out io.Writer) error {
	if path == "" {
		path = config.ProjectPath("")
	}

	if path == "" {
		return fmt.Errorf("aucun fichier de projet trouvé dans le dossier courant")
	}

	pcfg, err := config.LoadProject(path)
	if err != nil {
		return err
	}

	if err := config.Trust(pcfg.Path, pcfg.Raw); err != nil {
		return err
	}

	fmt.Fprintf(out, "approuvé : %s\n", pcfg.Path)

	return nil
}
