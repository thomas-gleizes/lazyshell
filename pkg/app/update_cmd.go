package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/update"
	"github.com/thomas-gleizes/lazyshell/pkg/version"
)

// updateTimeout bounds the whole command — resolving the tag, downloading the
// archive and its checksums. Generous, because it covers a release download on a
// slow link, but bounded: a hung TCP connection must not leave the command
// waiting forever with nothing on screen.
const updateTimeout = 10 * time.Minute

// RunUpdate implements `lazyshell update`: replace the running binary with the
// latest GitHub release. `--check` only reports, `--force` reinstalls even when
// there is nothing newer to install.
func RunUpdate(opts Options, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	return runUpdate(ctx, update.Updater{}, version.Version, opts, out)
}

// runUpdate is RunUpdate with its outside world injected: the tests point the
// Updater at an httptest server and a temporary file, and pass the installed
// version rather than reading the build-time one.
func runUpdate(ctx context.Context, u update.Updater, current string, opts Options, out io.Writer) error {
	// Checked before any request: telling someone their platform has no build
	// after making them wait for a download would be the wrong order.
	if _, err := u.ArchiveName(); err != nil {
		return err
	}

	latest, err := u.Latest(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "version installée : %s\n", current)
	fmt.Fprintf(out, "dernière version  : %s\n", latest)

	cmp, comparable := update.Compare(current, latest)

	switch {
	case opts.Check:
		switch {
		case !comparable:
			fmt.Fprintf(out, "%s n'est pas une version publiée : « lazyshell update --force » installera %s.\n",
				current, latest)
		case cmp < 0:
			fmt.Fprintln(out, "une mise à jour est disponible : lazyshell update")
		default:
			fmt.Fprintln(out, "déjà à jour")
		}

		return nil

	// A binary built from source (version "dev", or a `git describe` string)
	// is not something an update can be "newer" than, and silently replacing
	// it with a release would throw away exactly what its owner built. Saying
	// so and requiring --force is the only honest answer.
	case !comparable && !opts.Force:
		return fmt.Errorf("%s n'est pas une version publiée (binaire compilé localement ?) : « lazyshell update --force » pour installer %s quand même",
			current, latest)

	case comparable && cmp >= 0 && !opts.Force:
		fmt.Fprintln(out, "déjà à jour")

		return nil
	}

	fmt.Fprintf(out, "téléchargement de %s…\n", latest)

	bin, err := u.Download(ctx, latest)
	if err != nil {
		return err
	}

	path, err := u.Install(bin)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "installé : %s (%s → %s)\n", path, current, latest)

	// Sessions started before the swap keep running the old binary's process
	// image — which is fine, and worth saying, because "I updated and the help
	// popup still says the old version" is otherwise a bug report.
	fmt.Fprintln(out, "relancez lazyshell pour utiliser la nouvelle version.")

	return nil
}
