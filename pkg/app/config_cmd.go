package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
)

// userConfigTemplate is what `lazyshell config init` writes: every option, at
// its default value, with the comment that explains it. Deliberately exhaustive
// rather than minimal — the point of the sub-command is that configuring
// lazyshell never requires opening the README first.
//
// Every value here must be an actual default: config_cmd_test.go loads this
// template and compares the result to config.Default(), so shipping an example
// that quietly changes someone's configuration is not possible. The same test
// checks it produces no warnings — an example our own validation rejects would
// be worse than no example at all.
//
// The README carries an English rendering of the same content, checked
// key-by-key against this struct by pkg/config's doc_test.go rather than
// compared verbatim: the two files are not in the same language.
const userConfigTemplate = `# ~/.config/lazyshell/config.yml — configuration utilisateur de lazyshell.
#
# Toutes les clés sont facultatives : ce fichier montre chaque option à sa
# valeur par défaut. Supprimez ce que vous ne changez pas, une clé absente garde
# son défaut.
#
# Précédence, du plus faible au plus fort :
#   défauts < ce fichier < lazyshell.yml du projet < variables d'environnement < flags
#
# ` + "`lazyshell config show`" + ` affiche la configuration réellement appliquée.

# Langue de l'interface : "fr" ou "en".
# Lue et validée, mais pas encore appliquée : l'interface est en français.
language: fr

# Commande lancée derrière le pty de chaque session.
# Vide = $SHELL, à défaut /bin/bash.
shell: ""

# Valeur de TERM annoncée aux sessions.
term: xterm-256color

# Nombre maximum de lignes conservées par session une fois sorties de l'écran.
scrollback_size: 10000

# Largeur (en colonnes) de la liste des sessions en mode paysage.
sessions_panel_width: 40

# Hauteur (en lignes) de la liste des sessions en mode portrait.
sessions_panel_height: 10

# Bascule en mode portrait — panneaux empilés au lieu d'être côte à côte —
# quand le terminal fait au plus portrait_max_width colonnes ET plus de
# portrait_min_height lignes.
portrait_max_width: 84
portrait_min_height: 45

# Période de rafraîchissement de l'affichage, en millisecondes (10 à 1000).
# Un panneau inchangé n'est jamais redessiné : au repos le coût reste nul.
refresh_interval_ms: 30

# Temps d'attente, en millisecondes, après le SIGTERM d'une session avant
# d'escalader en SIGKILL.
kill_timeout_ms: 2000

# Préfixe d'échappement du mode pass-through (syntaxe gocui.Parse).
# $LAZYSHELL_PREFIX le surcharge à l'exécution — utile si un multiplexeur
# au-dessus de lazyshell mange déjà Ctrl-B.
prefix_key: Ctrl+B

# Remappage des actions. Une action absente garde sa touche par défaut.
keybindings:
  new_session: "n"
  new_session_in_dir: "N"
  kill_session: "x"
  rename_session: "r"
  duplicate_session: "c"
  restart_session: "R"
  zoom: "z"
  filter_sessions: "/"
  export_session: "w"
  toggle_broadcast: "b"
  select_next: "j"
  select_prev: "k"
  cycle_focus: Tab
  help: "?"
  quit: "q"

# Marqueurs de la gouttière, à gauche de chaque ligne de session.
# Un caractère au plus ; "" désactive le marqueur.
markers:
  bell: "!"        # la session a émis un BEL depuis la dernière fois qu'on l'a regardée
  alt_screen: "#"  # une application plein écran (vim, htop, less) a la main
  activity: "●"    # la session a produit de la sortie depuis la dernière fois qu'on l'a regardée
  broadcast: "+"   # la session est marquée pour recevoir la diffusion de saisie

# Pas de défilement du panneau de sortie.
scroll:
  page_lines: 0        # PgUp/PgDn : 0 = une page entière
  half_page_divisor: 2 # Ctrl-U/Ctrl-D : hauteur du panneau divisée par cette valeur

# Souris. Activée par défaut, et le seul intérêt de pouvoir la couper est
# qu'elle n'est pas gratuite : gocui donne la même valeur aux boutons de la
# souris et à Maj-Haut/Maj-Bas, donc tant qu'elle est active ces deux touches
# ne sont plus transmises à la session (voir docs/adr/0003-souris.md).
#
# La molette fait toujours défiler le contenu du panneau, jamais l'historique
# des commandes : elle n'est jamais encodée en flèches. Elle n'est transmise à
# l'application de la session que si celle-ci l'a explicitement réclamée
# (vim avec « set mouse=a », htop) — un shell ou un CLI d'agent ne le fait
# jamais.
mouse:
  enabled: true
  wheel_lines: 3      # lignes défilées par cran de molette
  forward_to_app: true

# Couleurs : un nom ANSI de terminal (black, red, green, yellow, blue, magenta,
# cyan, white, chacun préfixable par "bright"), un nom W3C/CSS ("navy", "teal",
# "#rrggbb"...), ou "default" pour la couleur propre du terminal.
#
# Les deux tables se recoupent et se contredisent : en CSS, "blue" désigne
# #0000FF, qu'un terminal affiche en bleu vif. lazyshell résout d'abord les
# noms ANSI, donc "blue" donne le bleu ordinaire ; écrivez "navy" pour le bleu
# CSS, ou "brightblue" pour le bleu vif du terminal.
theme:
  active_border_color: green
  inactive_border_color: default
  selected_bg_color: blue
  pass_through_border_color: red

# Copie en mode copy-mode : vide = OSC 52 uniquement (traverse SSH, aucun
# binaire requis). Aucun moyen de détecter si le terminal l'accepte : si ce
# n'est pas le cas, indiquez ici une commande qui lira le texte sur son stdin
# (ex. "pbcopy" ou "xclip -selection clipboard").
clipboard:
  fallback_command: ""
`

// InitUserConfig implements `lazyshell config init`.
func InitUserConfig(path string, out io.Writer) error {
	if path == "" {
		return errors.New("impossible de résoudre le chemin de la configuration (ni $LAZYSHELL_CONFIG, ni $XDG_CONFIG_HOME, ni un home)")
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("création de %s: %w", dir, err)
		}
	}

	// O_EXCL rather than a Stat-then-Write, same reasoning as InitProject: this
	// file is hand-edited, and clobbering it is the single worst thing this
	// sub-command could do.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s existe déjà", path)
		}

		return fmt.Errorf("écriture de %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.WriteString(f, userConfigTemplate); err != nil {
		return fmt.Errorf("écriture de %s: %w", path, err)
	}

	fmt.Fprintf(out, "créé : %s\n", path)

	return nil
}

// ShowConfig implements `lazyshell config show`: the configuration lazyshell
// would actually run with, after every layer of the precedence chain has had
// its say, preceded by which sources were really read.
//
// This exists because every other answer to "why is my key not taking effect"
// requires reading the source. A typo lands in the warnings, an out-of-range
// value has been silently corrected, a project file may have overridden the
// shell, and $LAZYSHELL_PREFIX beats the file — none of which is visible from
// the file itself.
func ShowConfig(opts Options, out, errOut io.Writer) error {
	path := config.Path()

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	sources := []string{"# défauts intégrés"}

	if _, statErr := os.Stat(path); statErr == nil {
		sources = append(sources, "# fichier utilisateur : "+path)
	} else {
		sources = append(sources, "# fichier utilisateur : "+path+" (absent, défauts utilisés)")
	}

	problems := checkConfig(&cfg, errOut)

	cfg, pcfg, projectErrs := loadProject(opts, cfg, errOut)
	if pcfg.Path != "" {
		sources = append(sources, "# fichier de projet : "+pcfg.Path)
	}

	problems = append(problems, projectErrs...)

	// $LAZYSHELL_PREFIX is applied by pkg/gui at startup, not by config.Load, so
	// the merged struct alone would misreport it — the one place where showing
	// the config as loaded would be a lie.
	if env := os.Getenv("LAZYSHELL_PREFIX"); env != "" {
		if _, _, perr := gui.ParseKey(env); perr == nil {
			cfg.PrefixKey = env
			sources = append(sources, "# $LAZYSHELL_PREFIX : "+env)
		}
	}

	for _, source := range sources {
		fmt.Fprintln(out, source)
	}

	fmt.Fprintln(out)

	// Filled in before printing: the theme colors and the keys of actions nobody
	// remapped have their real defaults in pkg/gui, not in the merged struct,
	// which would otherwise report an empty theme for a visibly green border.
	encoded, err := yaml.Marshal(gui.Effective(cfg))
	if err != nil {
		return fmt.Errorf("sérialisation de la configuration: %w", err)
	}

	if _, err := out.Write(encoded); err != nil {
		return err
	}

	if len(problems) > 0 {
		fmt.Fprintf(out, "\n# %d problème(s) signalé(s) ci-dessus ; les valeurs affichées sont celles réellement appliquées.\n",
			len(problems))
	}

	return nil
}
