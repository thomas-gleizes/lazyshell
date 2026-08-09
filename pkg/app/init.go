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

# env_files:                # optionnel — fichiers .env chargés pour TOUTES les
#   - .env                  # sessions ci-dessous, dans l'ordre (le dernier
#   - .env.local             # gagne sur le précédent en cas de clé commune)
# no_default_env: true      # optionnel — désactive le ".env" auto du cwd de
#                            # chaque session (chargé par défaut sinon)

# Les groupes rassemblent les sessions sous un en-tête dans la liste de gauche,
# et permettent de les piloter en bloc (A diffuse une saisie au groupe, X le
# tue, W le relance, G le filtre). Déclarer un groupe ici n'est pas obligatoire
# — c'est ce qui fixe l'ORDRE des en-têtes ; un groupe cité par une session
# sans être déclaré s'affiche après ceux-ci. Les sessions sans groupe passent
# en dernier.
groups:
  - name: services
  - name: agents

sessions:
  - name: api
    group: services
    # cwd est relatif à CE fichier, pas au dossier depuis lequel on lance
    # lazyshell ; ~ est étendu. Absent, il vaut le dossier de ce fichier.
    cwd: .
    # command est tapée dans le shell une fois qu'il est prêt, pas exécutée à sa
    # place : quand elle se termine (ou après un Ctrl-C), le shell reste là.
    command: echo "remplacez-moi"
    env:
      PORT: "3000"
    # env_files:             # optionnel — s'ajoutent à ceux du dessus, pour
    #   - .env.api            # cette session seulement
    # no_default_env: false  # optionnel — recharge le ".env" auto même si le
    #                        # projet l'a désactivé plus haut

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

// agentHookConfig is `init --agents`'s output: two copy-pasteable config
// fragments, one per adaptateur this phase ships. Not a file lazyshell
// writes itself — unlike lazyshell.yml, there is no single right path
// (settings.json/config.toml may already exist with content of their own to
// merge into), so printing to stdout for the user to paste is the only
// choice that cannot clobber something.
const agentHookConfig = `# À coller dans .claude/settings.json (fusionner avec un bloc "hooks"
# existant s'il y en a un déjà) :
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "lazyshell hook working"}]}],
    "Notification":     [{"hooks": [{"type": "command", "command": "lazyshell hook blocked"}]}],
    "Stop":             [{"hooks": [{"type": "command", "command": "lazyshell hook done"}]}]
  }
}

# À coller dans ~/.codex/config.toml (Codex n'a qu'un seul événement,
# agent-turn-complete — pas de distinction working/blocked) :
notify = ["lazyshell", "hook", "done"]
`

// PrintAgentHookConfig implements `lazyshell init --agents`: prints the hook
// config for every adaptateur this phase ships (Claude Code, Codex — see
// RAPPORT_ANALYSE_INTEGRATION_AGENTS_IA.md for why opencode is not among
// them yet) instead of writing lazyshell.yml.
func PrintAgentHookConfig(out io.Writer) error {
	fmt.Fprintln(out, "Ces hooks ne font rien si la session ne tourne pas sous lazyshell")
	fmt.Fprintln(out, "(il leur faut $LAZYSHELL_SOCK, que lazyshell seul définit) :")
	fmt.Fprintln(out)

	_, err := io.WriteString(out, agentHookConfig)

	return err
}
