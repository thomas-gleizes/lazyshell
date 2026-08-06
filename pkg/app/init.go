package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// projectTemplate is what `lazyshell init` writes. It is deliberately a fully
// commented example rather than a minimal stub: the point of the sub-command is
// that you never have to open the README to remember the schema.
const projectTemplate = `# lazyshell.yml — sessions démarrées automatiquement dans ce dossier.
#
# Les commandes de ce fichier sont exécutées au lancement : lazyshell demande
# une approbation la première fois, et de nouveau à chaque modification du
# fichier (` + "`lazyshell allow`" + ` pour approuver sans lancer l'interface,
# ` + "`lazyshell --no-autostart`" + ` pour ouvrir sans rien démarrer).
#
# Seules les clés ci-dessous sont lues dans un fichier de projet : le thème et
# les raccourcis clavier restent ceux de la config utilisateur.

# shell: /bin/zsh          # optionnel — surcharge la config utilisateur ici

sessions:
  - name: api
    # cwd est relatif à CE fichier, pas au dossier depuis lequel on lance
    # lazyshell ; ~ est étendu. Absent, il vaut le dossier de ce fichier.
    cwd: .
    # command est tapée dans le shell une fois qu'il est prêt, pas exécutée à sa
    # place : quand elle se termine (ou après un Ctrl-C), le shell reste là.
    command: echo "remplacez-moi"
    env:
      PORT: "3000"

  - name: shell            # sans command : un simple shell dans le cwd
`

// InitProject implements `lazyshell init`.
func InitProject(dir string, out io.Writer) error {
	path := filepath.Join(dir, config.ProjectFileNames[0])

	// O_EXCL rather than a Stat-then-Write: this file is hand-edited and holds
	// commands the user approved, so clobbering an existing one is the single
	// worst thing this sub-command could do.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s existe déjà", path)
		}

		return fmt.Errorf("écriture de %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.WriteString(f, projectTemplate); err != nil {
		return fmt.Errorf("écriture de %s: %w", path, err)
	}

	fmt.Fprintf(out, "créé : %s\n", path)

	return nil
}
