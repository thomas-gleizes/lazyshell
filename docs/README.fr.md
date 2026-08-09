# lazyshell

`lazyshell` est un gestionnaire de sessions shell à la `tmux`/`screen`, avec une
interface TUI à deux panneaux dans l'esprit de `lazygit`/`lazydocker` : la liste
de vos sessions à gauche, la sortie en direct de celle qui est sélectionnée à
droite. Les sessions continuent de tourner en arrière-plan — historique conservé
— même pendant que vous en regardez une autre.

Le panneau de sortie est un véritable émulateur de terminal : les applications
plein écran fonctionnent, `vim`, `htop` et `less` tournent à l'intérieur d'une
session, curseur et couleurs compris.

![démo](demo.gif)

🇬🇧 **English version of this document: [`README.md`](../README.md) (the
reference version).**

📖 **Documentation en ligne : [thomas-gleizes.github.io/lazyshell](https://thomas-gleizes.github.io/lazyshell/)**
— installation, utilisation, configuration, sessions d'agents IA et configuration
de projet, en [français](https://thomas-gleizes.github.io/lazyshell/fr/) et en
[anglais](https://thomas-gleizes.github.io/lazyshell/en/). Les sources du site
vivent dans `site/`.

## Installation

Binaire précompilé, sans toolchain Go : télécharger l'archive de son OS/arch
depuis la [page des releases](https://github.com/thomas-gleizes/lazyshell/releases),
vérifier `checksums.txt`, puis extraire `lazyshell` dans un dossier de son `PATH` :

```sh
tar xzf lazyshell_<os>_<arch>.tar.gz
sudo mv lazyshell /usr/local/bin/
lazyshell --version
```

Ou, pour que ce téléchargement/vérification/extraction soit fait pour vous
(Linux et macOS, amd64/arm64) :

```sh
curl -fsSL https://raw.githubusercontent.com/thomas-gleizes/lazyshell/main/scripts/install.sh | bash
```

Avec Go installé :

```sh
go install github.com/thomas-gleizes/lazyshell/cmd/lazyshell@latest
```

Ou depuis les sources :

```sh
git clone https://github.com/thomas-gleizes/lazyshell.git
cd lazyshell
make build   # produit ./bin/lazyshell
```

## Utilisation

Lancer `lazyshell` dans un terminal. `Tab` change le panneau actif entre la liste
des sessions et le panneau de sortie, et `→` / `←` font la même chose de façon
directionnelle ; quand le panneau de sortie a le focus, les frappes vont
directement au shell de cette session (mode « pass-through »).
Appuyer sur `?` à tout moment ouvre une aide dans l'application, qui liste tous
les raccourcis ci-dessous.

| Touche | Action |
| --- | --- |
| `q` / `Ctrl+C` | Quitter lazyshell |
| `Tab` | Changer de panneau actif |
| `→` | Aller au panneau de sortie |
| `?` | Afficher l'aide |
| `j` / `↓` | Session suivante |
| `k` / `↑` | Session précédente |
| `n` | Nouvelle session |
| `x` / `d` | Tuer la session sélectionnée |
| `D` | Supprimer définitivement la session sélectionnée (retirée du panneau) |
| `r` | Renommer la session sélectionnée |
| `c` | Dupliquer la session sélectionnée |
| `N` | Nouvelle session en demandant son nom (vide = nom automatique) |
| `M` | Nouvelle session dans un dossier choisi |
| `w` | Exporter le scrollback de la session sélectionnée vers un fichier |
| `b` | Marquer/démarquer la session pour la diffusion (broadcast) |
| `B` | Sauter à la prochaine session d'agent bloquée |
| `g` | Affecter la session sélectionnée à un groupe, choisi dans une liste ou tapé au clavier |
| `G` | N'afficher que le groupe de la session sélectionnée ; à nouveau pour annuler |
| `A` | Marquer tout le groupe pour la diffusion, ou le démarquer |
| `X` | Tuer toutes les sessions du groupe |
| `W` | Relancer les sessions terminées du groupe |
| `F12` | Afficher/masquer le panneau de debug (ne fait quelque chose qu'avec `--debug`) |

Quand c'est le **panneau de sortie** qui a le focus, ce sont ces touches-ci qui
s'appliquent :

| Touche | Action |
| --- | --- |
| `←` | Revenir à la liste des sessions (sauf pendant le pass-through) |
| `i` / `Enter` | Donner le clavier au shell (mode pass-through) |
| `Ctrl+O` (configurable) | Reprendre le clavier, depuis le mode pass-through |
| `Esc` `Esc` | Pareil, sans touche à apprendre : deux Échap d'affilée, en moins de 400 ms |
| `PgUp` / `PgDn` | Défiler d'un écran dans l'historique |
| `Ctrl+U` / `Ctrl+D` | Défiler d'un demi-écran |
| `/` | Rechercher dans l'historique ; `n` / `N` pour l'occurrence suivante/précédente |
| `v` | Démarrer (ou étendre) une sélection de lignes — mode copie |
| `y` ou un second `v` | Copier la sélection (OSC 52, ou la commande de repli configurée) |
| `{` / `}` | Sauter au prompt précédent/suivant (nécessite l'intégration shell OSC 133) |
| `Y` | Copier la sortie de la dernière commande terminée (nécessite l'intégration shell OSC 133) |
| `Esc` | Quitter la recherche ou annuler la sélection en cours |

Démarrer une session (`n`, `N`, `c`) ou en relancer une (`R`) vous dépose
directement dedans : le panneau de sortie prend le focus et le pass-through est
armé, on peut taper tout de suite. `Ctrl+O` reprend le clavier. Déplacer la
sélection avec `j` / `k` est de la navigation et ne fait jamais ça.

Deux `Esc` d'affilée, à moins de 400 ms l'un de l'autre, reprennent le clavier
eux aussi — la sortie qu'on trouve sans lire ce tableau. C'est un vrai double
appui : le premier `Esc` part dans la session comme n'importe quelle touche,
donc `Esc` continue de marcher dans `vim` et dans une session d'agent, et toute
autre touche tapée entre les deux casse la paire. La seule habitude qui n'y
survit pas est le double `Esc` réflexe de `vim`, qui fera sortir du
pass-through ; `Ctrl+O` reste la sortie pour qui préfère l'éviter.

Un shell qui se termine de lui-même — `exit`, `Ctrl+D`, ou ce qu'il faisait
tourner qui se finit — emmène l'interface avec lui : le pass-through est
désarmé et le focus revient au panneau des sessions, sur cette même session.
Elle reste sélectionnée et listée, terminée, donc `R` la relance et `x` / `D`
s'en débarrassent. Rien ne se passe derrière une popup : une confirmation ou
l'aide gardent le focus qu'elles ont.

Chaque panneau porte aussi ses touches les plus utilisées sur la ligne du bas de
son cadre, pour que les plus courantes soient lisibles sans ouvrir `?`. La liste
se raccourcit à ce qui tient dans la largeur du panneau, et celle du panneau de
sortie s'adapte à ce qu'il fait : seulement la sortie du pass-through quand le
pass-through est actif, aucune indication de défilement quand une application
plein écran tient la session.

### Lire la liste des sessions

Chaque session tient sur une ligne : une gouttière de quatre colonnes, puis son
nom, son état, son PID, et soit le titre de terminal posé par le shell
(généralement la commande en cours), soit son répertoire de travail.

| Marqueur | Signification |
| --- | --- |
| `!` | La session a sonné la cloche pendant que vous regardiez ailleurs. Effacé quand vous la sélectionnez. |
| `#` | Une application plein écran (`vim`, `htop`, `less`) tient la session. Affiché `[ALT]` dans la barre d'état pour la session sélectionnée. |
| `●` | La session a produit de la sortie alors qu'elle n'était pas à l'écran. Effacé quand vous la sélectionnez. |
| `+` | La session est marquée pour la diffusion — voir ci-dessous. |

Une session sans agent dont le shell a l'[intégration shell OSC
133](#intégration-shell-osc-133) activée affiche aussi `✗ <code>` devant son
titre/répertoire une fois que sa dernière commande a échoué — une session
d'agent porte déjà son propre marqueur d'état à la place, donc ça ne fait
jamais doublon avec lui.

### Groupes

Une session appartient à un groupe, ou à aucun. Les sessions groupées sont
affichées sous une ligne d'en-tête qui nomme le groupe, ce qui est précisément
ce qui rend lisible une liste de huit sessions d'agents IA : on voit d'un coup
d'œil lesquelles travaillent sur le même chantier. Les en-têtes sont purement
visuels — ils ne sont ni sélectionnables, ni cliquables, ni repliables, et
`j` / `k` passent directement par-dessus.

L'ordre est : les groupes déclarés par le fichier de projet, dans l'ordre où il
les déclare, puis les groupes créés à chaud dans leur ordre de première
apparition, puis les sessions sans groupe en dernier. Un groupe dont aucune
session n'est visible n'affiche aucun en-tête, donc un filtre qui masque tout un
groupe ne laisse pas de case vide derrière lui. Sans aucun groupe, il n'y a
aucun en-tête : la liste est exactement celle, plate, qu'elle a toujours été.

Déclarez les groupes et affectez-y les sessions dans `lazyshell.yml` (voir
[Configuration de projet](#configuration-de-projet)), ou posez-en un à tout
moment avec `g`, qui ouvre une liste : tous les groupes déjà utilisés, plus
« sans groupe » et « + nouveau groupe… » pour un nom qui n'y figure pas
encore. Quatre touches agissent ensuite sur tout le groupe de la
session sélectionnée : `A` y diffuse, `X` le tue, `W` relance celles de ses
sessions qui se sont terminées, et `G` restreint la liste à lui. Une action de
groupe atteint toujours tous ses membres, y compris ceux qu'un filtre masque —
« tuer le groupe » veut dire le groupe, pas la partie visible à l'écran.

`W` saute les sessions encore en cours plutôt que de refuser d'agir : un groupe
en partie terminé et en partie vivant est le cas normal. Contrairement au `R`
d'une seule session, il ne vous donne pas le clavier ensuite.

Les agents pilotent tout cela par le socket de contrôle — voir
[API de contrôle par les agents](#api-de-contrôle-par-les-agents).

### Diffusion (broadcast)

Marquer deux sessions ou plus avec `b`, puis s'attacher à l'une d'elles (`i` /
`Enter`) : chaque frappe part désormais vers toutes en même temps, pas seulement
vers celle que vous regardez. La barre d'état porte un avertissement
`⚠ BROADCAST → N sessions` tant que c'est armé, devant tout ce qu'elle dirait
autrement — c'est le seul état où une frappe dont on n'attend rien peut
atteindre plusieurs shells dans votre dos, alors il reste visible quoi qu'il
arrive. Démarquer une session (`b` à nouveau) la retire ; la diffusion s'arrête
d'elle-même dès qu'il en reste moins de deux marquées.

### Souris

Active par défaut. Cliquer une session la sélectionne — c'est de la navigation,
donc ça ne donne *pas* le clavier au shell ; le double-clic, si. La molette fait
défiler le contenu du panneau de sortie, et jamais l'historique de commandes du
shell : `lazyshell` traite la molette lui-même au lieu de laisser le terminal la
transformer en flèches, ce qui, à une invite, rappellerait la commande
précédente au lieu de défiler. Cliquer-glisser sélectionne des lignes, puis `y`
copie — relâcher le bouton ne copie rien tout seul.

Un programme dans une session ne reçoit la souris qu'une fois qu'il l'a demandée
(`vim` avec `set mouse=a`, `htop`) ; un shell ou une CLI d'agent IA ne la demande
jamais, donc la molette continue de faire défiler le scrollback. Mettre
`mouse.forward_to_app: false` garde la souris pour `lazyshell` quoi qu'il arrive.

Le seul coût d'avoir la souris activée : `Shift-Up` et `Shift-Down` ne sont plus
transmis à la session. `gocui` donne à ces touches et aux boutons de la souris
les mêmes valeurs, donc les deux ne peuvent pas marcher ensemble — voir
l'[ADR 0003](adr/0003-souris.md). Mettre `mouse.enabled: false` les récupère, au
prix des gestes ci-dessus.

Tant qu'une application plein écran a le contrôle, remonter dans l'historique —
et le mode copie, qui sélectionne dans ce même historique — est désactivé :
l'écran alternatif n'alimente pas le scrollback, et ces touches appartiennent à
l'application. `lazyshell` ne change jamais de mode tout seul : utiliser `i` ou
`Enter` pour donner le clavier au shell, et la touche de préfixe pour le
reprendre.

### Intégration shell (OSC 133)

Un shell qui émet les marques standard `OSC 133;A/B/C/D` autour de chaque
prompt et de chaque commande — zsh, fish et bash le supportent tous, en
général derrière une ligne à ajouter au fichier de démarrage du shell
(chercher « shell integration » ou « semantic prompt » dans la doc de votre
shell/prompt) — débloque trois choses sans configuration supplémentaire :

- `{` / `}` sautent au prompt précédent/suivant dans le scrollback.
- `Y` copie la sortie de la dernière commande terminée en une frappe, sans
  entrer en mode copie.
- La liste des sessions affiche `✗ <code>` pour une session sans agent dont la
  dernière commande a échoué (voir [Lire la liste des
  sessions](#lire-la-liste-des-sessions)), et une notification de bureau se
  déclenche de la même façon qu'une session d'agent `blocked`/`done` (voir
  [Notifications](#notifications-saut-vers-ce-qui-attend-et-stats-de-tour)) —
  pour une session sans agent seulement, puisqu'une session d'agent a déjà la
  sienne.

Rien ici n'a besoin d'un hook câblé comme pour [l'état des
agents](#état-autoritatif-via-des-hooks) : un shell avec l'intégration activée
émet ces marques tout seul, et un shell sans elle ne déclenche simplement
jamais rien de tout ça — aucun marqueur à désactiver, aucune clé de config à
éteindre, rien à détecter du tout. Seul le glyphe `✗` de la liste des sessions
est configurable (`markers.command_failed`, voir le tableau de référence
ci-dessous) ; le reste n'a ni glyphe ni touche à remapper en dehors des trois
identifiants d'action (`jump_prev_prompt`, `jump_next_prompt`,
`copy_last_output`) dans la map de raccourcis.

Les marques sont suivies par session, survivent à la troncature du scrollback
(voir l'[ADR 0008](adr/0008-integration-shell-osc-133.md) pour le comment), et
sont suspendues tant qu'une application plein écran tient la session — le même
principe « ça signale, ça ne change jamais de mode tout seul » que la souris
et le mode copie ci-dessus : rien de ce que tape `vim` ou `htop` n'est jamais
pris pour un prompt ou une borne de commande du shell.

## Configuration

`lazyshell config init` écrit un fichier de configuration entièrement commenté au
bon endroit, et `lazyshell config show` affiche la configuration réellement en
vigueur — une fois que chaque couche ci-dessous a eu son mot à dire — avec les
sources d'où elle vient. Cette seconde commande est la réponse à « pourquoi mon
réglage n'est-il pas pris en compte ».

`lazyshell` lit son fichier YAML depuis (première correspondance gagnante) :

1. `$LAZYSHELL_CONFIG`, s'il est défini
2. `$XDG_CONFIG_HOME/lazyshell/config.yml`
3. `~/.config/lazyshell/config.yml`

Un fichier absent n'est pas une erreur — lazyshell tourne alors avec ses valeurs
par défaut. Un fichier partiel n'a besoin de mentionner que les champs qu'il veut
surcharger ; tout le reste garde sa valeur par défaut.

Précédence, du plus faible au plus fort :

```
valeurs par défaut  <  ~/.config/lazyshell/config.yml  <  lazyshell.yml de projet
                    <  variables d'environnement  <  options de ligne de commande
```

Rien dans un fichier de configuration ne peut empêcher lazyshell de démarrer. Une
clé inconnue, une valeur hors bornes, un raccourci illisible ou une couleur
inconnue sont chacun signalés sur stderr avant l'ouverture de l'interface, et la
valeur par défaut est utilisée à la place — jamais un no-op silencieux, jamais un
refus de tourner.

### Référence

| Clé | Type | Défaut | Effet |
| --- | --- | --- | --- |
| `language` | `fr` \| `en` | `fr` | Langue de l'interface : raccourcis, popups, barre d'état, pieds de panneaux et messages de session. La sortie CLI (`lazyshell config ...`) reste en français. |
| `shell` | chaîne | `""` | Commande lancée derrière le pty de chaque session. Vide signifie `$SHELL`, avec repli sur `/bin/bash`. |
| `term` | chaîne | `xterm-256color` | `TERM` annoncé aux sessions. L'abaisser fait dégrader les programmes exprès. |
| `scrollback_size` | entier ≥ 0 | `10000` | Lignes gardées par session une fois sorties de l'écran. |
| `sessions_panel_width` | entier ≥ 5 | `40` | Largeur de la liste des sessions, en colonnes, en mode paysage. |
| `sessions_panel_height` | entier ≥ 5 | `10` | Hauteur de la liste des sessions, en lignes, en mode portrait. |
| `portrait_max_width` | entier | `84` | Le mode portrait s'applique à cette largeur de terminal ou en dessous… |
| `portrait_min_height` | entier | `45` | …et au-dessus de cette hauteur. Le portrait empile les panneaux au lieu de les mettre côte à côte. |
| `refresh_interval_ms` | entier, 10–1000 | `30` | Période de redessin. Un panneau inchangé n'est jamais poussé, donc le coût au repos reste proche de zéro quelle que soit la valeur. |
| `kill_timeout_ms` | entier ≥ 100 | `2000` | Attente après `SIGTERM` avant de passer à `SIGKILL`, puis à nouveau avant d'abandonner. |
| `prefix_key` | spec de touche | `Ctrl+O` | Touche de sortie du pass-through : une pression, on sort. Doit être une touche de contrôle, et elle ne peut plus être tapée dans une session. `$LAZYSHELL_PREFIX` la surcharge. |
| `keybindings` | map | voir plus bas | Remappe un identifiant d'action vers une spec de touche. Une action omise garde sa touche par défaut. |
| `markers.bell` | 0–1 caractère | `!` | Marqueur de gouttière pour une session qui a sonné pendant qu'elle était cachée. `""` le désactive. |
| `markers.alt_screen` | 0–1 caractère | `#` | Marqueur pour une session faisant tourner une application plein écran. `""` le désactive. |
| `markers.activity` | 0–1 caractère | `●` | Marqueur pour une session ayant produit de la sortie pendant qu'elle était cachée. `""` le désactive. |
| `markers.broadcast` | 0–1 caractère | `+` | Marqueur pour une session marquée pour recevoir les frappes diffusées. `""` le désactive. |
| `markers.agent_idle` | 0–1 caractère | `·` | Marqueur pour une session d'agent IA détectée, au repos. `""` le désactive. |
| `markers.agent_working` | 0–1 caractère | `…` | Marqueur pour une session d'agent IA détectée, en train de travailler. `""` le désactive. |
| `markers.agent_blocked` | 0–1 caractère | `‼` | Marqueur pour une session d'agent IA détectée qui vous attend. `""` le désactive. |
| `markers.agent_done` | 0–1 caractère | `✓` | Marqueur pour une session d'agent IA détectée ayant fini son tour. `""` le désactive. |
| `markers.command_failed` | 0–1 caractère | `✗` | Marqueur (à côté de son code de sortie, dans les colonnes nom/état plutôt que dans la gouttière) pour une session sans agent dont la dernière commande — via l'[intégration shell OSC 133](#intégration-shell-osc-133) — a échoué. `""` le désactive. |
| `scroll.page_lines` | entier ≥ 0 | `0` | Lignes parcourues par `PgUp`/`PgDn`. `0` signifie une hauteur de panneau entière. |
| `scroll.half_page_divisor` | entier ≥ 1 | `2` | `Ctrl-U`/`Ctrl-D` parcourent la hauteur du panneau divisée par cette valeur. |
| `theme.active_border_color` | couleur | `green` | Bordure du panneau qui a le focus. |
| `theme.inactive_border_color` | couleur | `default` | Bordure de tous les autres panneaux. |
| `theme.selected_bg_color` | couleur | `blue` | Fond de la ligne sélectionnée dans la liste des sessions. |
| `theme.pass_through_border_color` | couleur | `red` | Bordure du panneau qui a le focus, en mode pass-through. |
| `theme.tab_active_color` | couleur | `green` | Onglet sélectionné dans la barre d'onglets du panneau de sortie. |
| `clipboard.fallback_command` | chaîne | `""` | Commande lancée avec le texte copié sur son stdin, à la place d'OSC 52, pour un terminal qui ne le gère pas. Il n'y a aucun moyen de détecter le support, c'est donc un interrupteur manuel : vide signifie OSC 52 seulement. |
| `notify.fallback_command` | chaîne | `""` | Commande lancée avec le texte de notification sur son stdin, à la place d'OSC 9/777, quand une session d'agent IA détectée passe en blocked ou done. Vide signifie OSC seulement. |
| `window_title.enabled` | booléen | `true` | Si le titre de fenêtre/onglet du terminal hôte suit la session sélectionnée (son nom, plus son titre OSC 0/2 en direct s'il y en a un), via OSC 0. |
| `mouse.enabled` | booléen | `true` | Support du clic, de la molette et du glisser. L'activer coûte le pass-through de `Shift-Up`/`Shift-Down` — gocui donne à ces touches et aux boutons de la souris les mêmes valeurs, donc les deux ne peuvent pas marcher ensemble. À `false` pour les récupérer. |
| `mouse.wheel_lines` | entier ≥ 1 | `3` | Lignes parcourues dans le panneau de sortie par un cran de molette. |
| `mouse.forward_to_app` | booléen | `true` | Si un programme dans une session peut recevoir la souris lui-même, et seulement une fois qu'il l'a demandée avec un DECSET 9/1000/1002/1003 (`vim` avec `set mouse=a`, `htop`). Un shell ou une CLI d'agent IA ne la demande jamais, donc la molette continue de faire défiler le scrollback de lazyshell. |
| `perf.refresh_interval_ms` | `0`, ou entier ≥ 100 | `5000` | Fréquence d'échantillonnage des processus de chaque session pour l'onglet ressources. Ça tourne en arrière-plan que l'onglet soit ouvert ou non, donc ses courbes remontent déjà plus loin que le moment où vous l'avez ouvert ; toutes les sessions sont échantillonnées en une passe, donc le coût ne croît pas avec leur nombre. `0` coupe complètement l'échantillonnage — c'est le seul travail périodique qui lance un processus, donc qui n'ouvre jamais l'onglet n'a pas à le payer. |
| `env_tab.mask_secrets` | booléen | `true` | Si l'onglet env du panneau de sortie masque la valeur des variables dont le nom ressemble à un identifiant (`TOKEN`, `SECRET`, `PASSWORD`, `AUTH`, `..._KEY`). Le panneau est aussi partageable qu'une capture d'écran ; à `false` pour voir les vraies valeurs. |
| `control.enabled` | booléen | `false` | Si l'API de contrôle par les agents est ouverte — le socket que `lazyshell ctl` pilote. Désactivée par défaut, et lire [API de contrôle par les agents](#api-de-contrôle-par-les-agents) avant de l'activer : elle permet à tout processus tournant sous votre compte de créer des sessions, d'y taper et de lire leur sortie. |
| `agent_stats_command` | chaîne | `""` | Lancée pour la session d'agent IA sélectionnée, avec `$LAZYSHELL_SESSION_ID` dans son environnement ; la première ligne de sa sortie standard est affichée à côté de la durée du tour. Vide la désactive. |

Les specs de touches utilisent la syntaxe de `gocui.Parse` : un caractère seul
(`n`), ou `Ctrl+N`, `Alt+Space`, `Tab`, `Esc`.

Les couleurs acceptent :

- un **nom de couleur ANSI de terminal** — `black`, `red`, `green`, `yellow`,
  `blue`, `magenta`, `cyan`, `white`, et chacun préfixé de `bright`
  (`brightblue`, …). Ils veulent dire ce qu'ils veulent dire dans un terminal, et
  suivent la palette de votre terminal.
- un **nom de couleur W3C/CSS** (`navy`, `teal`, `chartreuse`, …) ou `#rrggbb`,
  pour une couleur précise plutôt qu'un emplacement de palette.
- `default`, pour la couleur par défaut du terminal.

Les deux jeux de noms se recouvrent et se contredisent : en CSS, `blue` vaut
`#0000FF`, que le terminal affiche en bleu *vif*. lazyshell résout d'abord les
noms ANSI, donc `blue` donne le bleu ordinaire ; écrire `navy` pour celui de CSS,
ou `brightblue` pour l'emplacement vif du terminal.

Les identifiants d'action remappables sont `new_session`, `new_named_session`, `new_session_in_dir`,
`kill_session`, `delete_session`, `rename_session`, `duplicate_session`,
`restart_session`, `zoom`, `filter_sessions`, `export_session`,
`toggle_broadcast`, `jump_next_blocked`, `jump_prev_prompt`, `jump_next_prompt`,
`copy_last_output`, `next_tab`, `prev_tab`,
`toggle_debug`, `select_next`, `select_prev`,
`cycle_focus`, `help` et `quit`. Un identifiant hors de cette liste est signalé
plutôt qu'ignoré.

### Exemple

C'est ce qu'écrit `lazyshell config init` — chaque option à sa valeur par défaut,
donc on peut supprimer tout ce qu'on ne change pas.

```yaml
# ~/.config/lazyshell/config.yml

language: fr
shell: ""
term: xterm-256color
scrollback_size: 10000

sessions_panel_width: 40
sessions_panel_height: 10
portrait_max_width: 84
portrait_min_height: 45

refresh_interval_ms: 30
kill_timeout_ms: 2000

prefix_key: Ctrl+O

keybindings:
  new_session: "n"
  new_named_session: "N"
  new_session_in_dir: "M"
  kill_session: "x"
  delete_session: "D"
  rename_session: "r"
  duplicate_session: "c"
  restart_session: "R"
  zoom: "z"
  next_tab: "]"
  prev_tab: "["
  filter_sessions: "/"
  export_session: "w"
  toggle_broadcast: "b"
  jump_next_blocked: "B"
  jump_prev_prompt: "{"
  jump_next_prompt: "}"
  copy_last_output: "Y"
  toggle_debug: F12
  select_next: "j"
  select_prev: "k"
  cycle_focus: Tab
  help: "?"
  quit: "q"

markers:
  bell: "!"
  alt_screen: "#"
  activity: "●"
  broadcast: "+"
  agent_idle: "·"
  agent_working: "…"
  agent_blocked: "‼"
  agent_done: "✓"
  command_failed: "✗"

scroll:
  page_lines: 0
  half_page_divisor: 2

theme:
  active_border_color: green
  inactive_border_color: default
  selected_bg_color: blue
  pass_through_border_color: red
  tab_active_color: green

clipboard:
  fallback_command: ""

notify:
  fallback_command: ""

window_title:
  enabled: true

mouse:
  enabled: true
  wheel_lines: 3
  forward_to_app: true

perf:
  refresh_interval_ms: 5000

env_tab:
  mask_secrets: true

control:
  enabled: false

agent_stats_command: ""
```

### Sessions d'agents IA

Une session dont le processus au premier plan est une CLI d'agent de code IA
connue (`claude`, `codex`, `opencode`) reçoit un marqueur de gouttière indiquant
son état détecté — `idle`, `working`, `blocked` (elle vous attend) ou `done` — au
lieu du seul marqueur d'activité générique, qui ne sait pas distinguer « elle a
produit de la sortie » de « elle attend une réponse ». La détection ne demande
aucune configuration : elle confronte les manifestes intégrés de
`pkg/agent/manifests` à l'écran visible de la session et à son titre de terminal.

Déposer un fichier `<nom-de-processus>.yml` dans `~/.config/lazyshell/agents/`
(ou l'équivalent `$XDG_CONFIG_HOME`) surcharge un manifeste intégré ou en ajoute
un pour un autre agent — un fichier du même nom qu'un manifeste intégré le
remplace entièrement, un nom différent s'ajoute au jeu. Voir les manifestes
intégrés pour le format. Les manifestes sont purement locaux ; lazyshell n'en
télécharge jamais un depuis le réseau.

#### État autoritatif via des hooks

La détection par manifeste est une supposition à partir de ce qui est à l'écran —
un second canal permet à l'agent d'annoncer son état franchement. Chaque session
a son propre socket Unix, exposé au processus qui tourne dedans sous
`$LAZYSHELL_SOCK` (à côté de `$LAZYSHELL_SESSION_ID`), et `lazyshell hook <état>`
— l'un de `idle`, `working`, `blocked` ou `done` — y écrit. C'est fait pour être
branché sur le mécanisme de hooks de l'agent, pas pour être tapé à la main :

```sh
lazyshell init --agents   # affiche la config à coller dans Claude Code / Codex
```

**Claude Code** — un bloc hooks dans `settings.json` : `UserPromptSubmit` →
`lazyshell hook working`, `Notification` → `lazyshell hook blocked`, `Stop` →
`lazyshell hook done`. **Codex** — une ligne `notify` dans `config.toml` ; Codex
n'a qu'un seul événement (`agent-turn-complete`), il ne peut donc jamais
rapporter que `done`. **opencode** n'est pas encore branché — son signal le plus
riche est un abonnement SSE plutôt que quelque chose qu'il pousse de lui-même,
une forme d'intégration différente, laissée pour plus tard.

Dès qu'une session a reçu un seul événement de hook, la supposition par
manifeste s'arrête définitivement pour cette session : le hook fait autorité à
partir de là, et pas seulement jusqu'au prochain changement d'écran. lazyshell
n'appelle jamais l'agent par ce socket — il ne fait qu'écouter, et la seule chose
qu'un événement de hook peut faire est de fixer cet unique état. Laisser un agent
faire autre chose que se décrire est une fonctionnalité distincte, désactivée par
défaut : voir [API de contrôle par les agents](#api-de-contrôle-par-les-agents).

#### API de contrôle par les agents

`control.enabled: true` ouvre un second socket — un par processus lazyshell, pas
un par session — que `lazyshell ctl` utilise pour piloter un lazyshell en cours.
C'est ce dont un agent « chef d'orchestre » a besoin : lister les autres
sessions, lire ce qu'elles ont affiché, en démarrer de nouvelles, y taper.

```sh
lazyshell ctl list                                # id, nom, statut, état d'agent, groupe
lazyshell ctl list --group agents                 # seulement ce groupe
lazyshell ctl read session-2 --tail 40            # texte brut, sans séquences d'échappement
lazyshell ctl new --name build --cwd ./api --command 'make test' --group agents
lazyshell ctl send build 'echo bonjour' --enter   # comme si c'était tapé
lazyshell ctl kill build
lazyshell ctl rename build tests
```

Les groupes (voir [Groupes](#groupes)) sont lisibles et modifiables depuis ici,
ce qui est justement ce qui permet à un agent d'en orchestrer plusieurs autres
comme un bloc :

```sh
lazyshell ctl group build agents                  # affecte une session à un groupe
lazyshell ctl ungroup build                       # l'en retire
lazyshell ctl group-send agents 'git pull' --enter
lazyshell ctl group-kill agents
```

Les deux verbes de diffusion affichent combien de sessions ils ont touchées. Un
groupe vide ou inexistant est une erreur, jamais un « 0 session » silencieux :
celui qui a fait une faute de frappe dans un nom de groupe ne doit pas s'entendre
dire que son kill a réussi. `group-send` saute les sessions déjà terminées.

Relancer un groupe n'est délibérément **pas** exposé : aucun verbe `restart`
n'existe pour une session seule non plus, et n'en ajouter un que pour les
groupes serait incohérent. Cela reste la touche `W`, dans l'interface.

Une session se désigne par son id (`session-2`, la valeur de
`$LAZYSHELL_SESSION_ID`) ou par son nom exact. `--json` affiche la réponse brute
au lieu du rendu lisible. Contrairement à `lazyshell hook`, qui sort toujours en
0 pour ne jamais casser le tour d'un agent, `ctl` sort en code non nul au moindre
échec : celui qui a demandé une session sans l'obtenir doit pouvoir le savoir.

`ctl new` ne vole ni la sélection ni le clavier, contrairement à la touche `n` :
un agent en tâche de fond qui crée une session ne doit pas arracher le curseur à
ce que vous étiez en train de taper.

**À lire avant d'activer.** Il n'y a ni jeton ni permission par session : les
permissions `0600` du socket sont l'intégralité du contrôle d'accès. Activer
signifie donc que *tout processus tournant sous votre compte* peut créer des
sessions, y taper des commandes et lire leur sortie — pas seulement les agents
que vous avez lancés dans lazyshell. `ctl read` rend le scrollback verbatim,
secrets compris ; le masquage de l'onglet env n'a pas d'équivalent ici, parce
qu'un identifiant affiché dans un shell est indiscernable de n'importe quel autre
texte une fois à l'écran.

C'est ce compromis qui justifie le défaut à `false`, et le choix d'un socket et
d'un protocole distincts plutôt qu'une extension du canal de hooks ci-dessus —
lequel reste ouvert par défaut précisément parce que tout ce qu'il peut faire est
de déplacer un marqueur dans une liste. Le raisonnement complet, et les
alternatives pesées, sont dans `docs/adr/0006-api-de-controle-par-les-agents.md`.

#### Notifications, saut vers ce qui attend, et stats de tour

Une session qui passe en `blocked` ou `done` déclenche une notification de
bureau — OSC 9 et OSC 777 vers le terminal hôte par défaut (les deux envoyés
inconditionnellement ; un terminal qui n'en comprend qu'un ignore l'autre), ou la
commande de `notify.fallback_command`, avec le texte de la notification sur son
stdin, pour un terminal qui en a besoin. Au-delà de deux ou trois sessions
d'agent ouvertes, `B` saute directement à la prochaine session `blocked`, en
cyclant et en bouclant — c'est tout l'intérêt du marqueur et de la notification.

Une session sans agent notifie de la même façon quand sa dernière commande —
via l'[intégration shell OSC 133](#intégration-shell-osc-133) — échoue, pour
qu'un build ou un script long n'ait pas besoin d'être surveillé pour savoir
qu'il a échoué. Une session d'agent ne déclenche jamais cette seconde
notification en plus de la sienne.

Une session en plein tour (`working`) affiche depuis combien de temps son tour
tourne dans la liste des sessions, par exemple `⏱ 1m32s`. Définir
`agent_stats_command` lance cette commande pour la session *sélectionnée*
seulement (au plus une fois toutes les 5 secondes — c'est fait pour un relevé de
tokens/coût, pas pour quelque chose d'assez léger pour tourner par session à
chaque tick) avec `$LAZYSHELL_SESSION_ID` dans son environnement, et affiche la
première ligne de sa sortie à côté de la durée — la même forme « commande
externe, on affiche sa ligne de sortie » que la `statusLine` de Claude Code.
lazyshell ne parse ni ne suit lui-même la consommation de tokens.

## Configuration de projet

Lancer `lazyshell` dans un dossier contenant un `lazyshell.yml` démarre les
sessions que ce fichier déclare — chacune dans son propre dossier, avec son
propre environnement et sa propre commande — au lieu de s'ouvrir vide.

`lazyshell init` écrit un point de départ commenté dans le dossier courant.

```yaml
# ./lazyshell.yml

# Optionnel : surcharge le shell de la config utilisateur, pour ce projet seulement.
shell: /bin/zsh

# Optionnel : fichiers .env chargés pour toutes les sessions ci-dessous, dans
# l'ordre — un fichier plus tardif surcharge une clé posée par un précédent.
env_files:
  - .env
  - .env.local

# Optionnel : déclare les groupes utilisés par ce projet et — la seule raison
# d'écrire ce bloc — l'ordre dans lequel leurs en-têtes apparaissent dans le
# panneau des sessions. Une session peut citer un groupe absent d'ici ; il se
# place simplement après les groupes déclarés.
groups:
  - name: services
  - name: agents

sessions:
  - name: api
    # Optionnel : le groupe dans lequel la session démarre. Modifiable ensuite
    # avec `g`.
    group: services
    # Relatif à *ce fichier*, pas à l'endroit d'où lazyshell a été lancé.
    # `~` est expansé. Omis, il vaut le dossier de ce fichier.
    cwd: ./services/api
    # Tapée dans le shell une fois qu'il est là, pas exécutée à sa place : quand
    # la commande se termine (ou qu'on l'interrompt par Ctrl-C), le shell est
    # toujours là.
    command: make dev
    env:
      PORT: "3000"
    # Optionnel : en plus des env_files ci-dessus, pour cette session seulement.
    env_files:
      - .env.api

  - name: web
    group: services
    cwd: ./web
    command: npm run dev

  - name: shell          # pas de groupe : affichée sous « sans groupe », en bas
```

Les sessions démarrent dans l'ordre du fichier, et la première est sélectionnée.
Une entrée qui ne valide pas (`name` vide ou dupliqué, `cwd` manquant) est
ignorée et signalée dans la barre d'état — les autres démarrent quand même. Il
en va de même d'une mauvaise entrée de `groups:` (`name` vide ou dupliqué) :
elle est écartée et signalée, et les groupes corrects s'appliquent quand même.

Un groupe déclare un nom et rien d'autre — pas de couleur, pas de glyphe, pas de
touche. C'est la même règle que pour le reste de ce fichier : un dépôt dit ce
qui existe, pas à quoi ressemble votre interface.

**Seuls `shell`, `env_files`, `no_default_env`, `groups` et `sessions` sont lus
depuis un fichier de projet.** `theme`, `keybindings`, `prefix_key` et le reste restent
sous votre seul contrôle : un dépôt que vous avez cloné ne doit pas pouvoir
remapper votre clavier. Les autres clés sont ignorées, avec un avertissement sur
stderr.

### Fichiers .env

Chaque session — déclarée dans un fichier de projet ou non — charge
automatiquement un `.env` depuis son propre répertoire de travail, s'il y en a
un. Par-dessus, en couches, chacune surchargeant une clé posée par la
précédente :

1. `<cwd de la session>/.env`, automatique, sauf s'il est désactivé (voir plus bas)
2. `--env-file <chemin>` (répétable, s'applique à toutes les sessions démarrées par ce lancement)
3. les `env_files:` du projet (s'appliquent à toutes les sessions déclarées)
4. les `env_files:` d'une session (cette session seulement)
5. la map `env:` de cette session — gagne toujours, sur tous les fichiers

Pour couper la recherche automatique de `<cwd>/.env`, passer `--no-env-file`
(toutes les sessions démarrées par ce lancement), mettre `no_default_env: true`
en haut d'un fichier de projet (toutes les sessions qu'il déclare), ou sur une
`SessionSpec` (cette session seulement — surchargeant le réglage du projet dans
un sens comme dans l'autre).

### Quel fichier est utilisé

1. `--config-file <fichier>` (`-f`)
2. `$LAZYSHELL_PROJECT_CONFIG`
3. `./lazyshell.yml`
4. `./.lazyshell.yml`

Seul le dossier courant est cherché — pas de remontée vers la racine d'un dépôt,
donc le fichier qui tourne est toujours celui qu'on voit.

### Approuver un fichier de projet

Un `lazyshell.yml` est versionné dans un dépôt : il exécuterait donc des
commandes arbitraires dès qu'on fait `cd` dans un clone. lazyshell demande une
fois, avant l'ouverture de l'interface, et retient la réponse par fichier — et
redemande dès que le contenu du fichier change :

```sh
lazyshell allow            # approuve le fichier du dossier courant, ne lance rien
lazyshell allow ./x.yml    # approuve un fichier précis
lazyshell --no-autostart   # ouvre l'interface sans rien démarrer
lazyshell --env-file .env.prod   # fichier .env en plus, pour toutes les sessions de ce lancement
lazyshell --no-env-file          # ignore le "<cwd>/.env" automatique de chaque session
```

Les approbations vivent dans `trust.yml` à côté de la config utilisateur. Quand
stdin n'est pas un terminal, l'approbation est refusée plutôt que supposée.

## Mode debug

Une fois que lazyshell tient le terminal, il n'y a plus nulle part où écrire :
stderr est inutilisable et la barre de statut fait une ligne. `--debug` est le
moyen de voir ce que l'interface croit qu'il se passe.

```sh
lazyshell --debug
```

Il fait deux choses à la fois. Il écrit à la suite de
`~/.config/lazyshell/debug.log` — à côté de `config.yml`, en `0600`, jamais
tronqué, pour pouvoir comparer deux lancements — et il ouvre un petit panneau
en haut à droite du panneau de sortie qui affiche les derniers évènements en
direct. `F12` masque et réaffiche ce panneau ; le fichier continue d'être écrit
dans les deux cas.

Trois sortes de lignes sont enregistrées :

| Étiquette | Ce que c'est |
| --- | --- |
| `KEY` | Une frappe telle que le panneau de sortie l'a reçue : son nom, les valeurs brutes `key`/`ch`/`mod`, ce que `Normalize` en a fait quand les deux diffèrent, et le mode dans lequel elle a atterri (pass-through, défilement, mode copie, recherche, un onglet) |
| `ACT` | Une action qui est partie — un raccourci clavier, un geste souris, ou une des branches que l'éditeur du panneau de sortie traite lui-même |
| `EVT` | Session créée / tuée / terminée, transitions d'état d'un agent, changements de sélection et d'onglet, redimensionnements |

Deux choses à savoir avant de lire un log :

- **Les touches ne sont enregistrées que pour le panneau de sortie.** gocui
  n'offre aucun hook clavier global : une touche pressée sur le panneau des
  sessions apparaît en ligne `ACT` si elle est liée, et pas du tout sinon.
- **`F12` n'atteint plus la session** tant que lazyshell tourne, mode debug ou
  non — c'est un raccourci global. Remapper `toggle_debug` dans la config si
  quelque chose que vous lancez en a besoin.

Le fichier contient toutes les frappes tapées dans un shell, y compris à
l'invite de mot de passe d'un programme qui ne coupe pas l'écho. C'est la
raison du `0600` ; le supprimer une fois qu'on n'en a plus besoin.

## Développement

```sh
make build   # go build -o bin/lazyshell ./cmd/lazyshell
make test    # go test -race ./...
make lint    # golangci-lint run
```
