# Roadmap — lazyshell

Gestionnaire de sessions shell TUI (type tmux) en Go / gocui, dérivé de l'analyse
`RAPPORT_ANALYSE_LAZYGIT_LAZYDOCKER.md`.

Principe directeur : **chaque phase produit un binaire lançable et testable à la main**. On ne
construit jamais deux couches spéculatives d'affilée. Le risque technique majeur (pty + gocui qui se
disputent le terminal) est attaqué en phase 1, pas en phase 4.

---

## Phase 0 — Socle du dépôt

**But** : `go run ./cmd/lazyshell` affiche une fenêtre gocui vide qu'on peut quitter proprement.

- `go mod init github.com/<user>/lazyshell` (Go 1.22+).
- Dépendances initiales : `github.com/jesseduffield/gocui`,
  `github.com/jesseduffield/lazycore/pkg/boxlayout`, `github.com/creack/pty`, `gopkg.in/yaml.v3`.
  (Réalisé : gocui n'est disponible qu'en pseudo-version `master`, qui **exige Go 1.25** — le
  plancher « Go 1.22+ » ci-dessus est inatteignable. Les autres dépendances sont ajoutées à la
  phase qui les utilise, `go mod tidy` supprimant toute dépendance non importée.)
- Arborescence minimale : `cmd/lazyshell/main.go`, `pkg/app`, `pkg/gui`, `pkg/session`, `pkg/tasks`.
- `pkg/app` : bootstrap (construit le `SessionManager`, construit le `Gui`, appelle `gui.Run()`).
- `pkg/gui/gui.go` : `gocui.NewGui`, `SetManager`, `MainLoop`, binding `q` / `Ctrl-C` → `ErrQuit`,
  restauration du terminal via `defer g.Close()`.
- Outillage : `Makefile` (`build`, `run`, `test`, `lint`), `.gitignore`, `golangci-lint`, CI GitHub
  Actions (build + vet + test sur linux/macos).

**Critère de sortie** : ouverture/fermeture sans laisser le terminal dans un état cassé (pas
d'écho perdu, pas de curseur invisible).

**Risque** : aucun. Phase mécanique.

---

## Phase 1 — Spike pty (la phase qui décide de tout)

**But** : valider *avant* d'investir dans l'UI que gocui et un pty interactif cohabitent. C'est le
seul point sans précédent dans lazygit/lazydocker.

Un binaire jetable `cmd/spike-pty` : une seule vue plein écran, un seul `bash` derrière un pty.

- `pty.Start(exec.Command(shell))` → `ptmx *os.File`.
- Goroutine de lecture : `io.Copy(view, ptmx)` (la `gocui.View` est un `io.Writer` thread-safe).
- `goEvery(30ms, reRender)` : si `view.IsTainted()`, `g.Update(noop)` pour déclencher le redraw.
- Clavier → `ptmx.Write()` : c'est **le** point dur. gocui livre des `gocui.Key`/rune, pas des
  octets bruts. Il faut une table de traduction (touches fléchées → séquences CSI `ESC[A`…,
  `Ctrl-<x>` → octet de contrôle, `Backspace` → `0x7f`, `Enter` → `\r`).
- `pty.Setsize` avec les dimensions réelles de la vue, recalculées dans `layout()`.

**Questions à trancher ici, à documenter dans un ADR :**

1. **Rendu des séquences ANSI.** gocui interprète un sous-ensemble de SGR (couleurs, gras), mais pas
   les séquences de positionnement de curseur (`ESC[H`, `ESC[2J`, mode alternate screen). Conséquence
   directe : `ls`, `git log`, un prompt coloré passent ; `vim`, `htop`, `less` **ne passeront pas**
   tels quels. Trois options, à décider maintenant :
   - (a) MVP « line-oriented » assumé : on documente que les applis plein écran ne sont pas
     supportées ;
   - (b) émulateur de terminal en amont (`github.com/hinshun/vt10x` ou `charmbracelet/x/vt`) : on
     maintient une grille de cellules, et on rend cette grille dans la vue à chaque frame au lieu
     d'`io.Copy` — coût réel, mais c'est la seule voie vers un vrai tmux ;
   - (c) filtrage/strip des séquences non supportées pour éviter l'affichage corrompu.
   **Recommandation : (a) pour les phases 1-4, avec l'interface de rendu conçue pour permettre (b)
   en phase 10** (c'est-à-dire : la session expose « donne-moi l'état à afficher », pas « voici un
   flux d'octets »).
2. **Qui possède le terminal** quand une session est en pass-through : gocui garde toujours le
   contrôle (on ne fait *pas* de `Suspend`/`Resume`, sinon on perd le multiplexage).
3. **Sortie du mode pass-through** : un préfixe d'échappement à la tmux (`Ctrl-A`, `Ctrl-B` ou
   `Ctrl-Space`) car `Tab` et `Esc` doivent partir au shell.

**Critère de sortie** : dans le spike, taper `ls -la`, `cd /tmp`, `echo $$`, `Ctrl-C` sur un `sleep`
fonctionne ; le redimensionnement du terminal propage la bonne taille (`stty size` dans la session
retourne les dimensions de la vue).

**Risque** : élevé — c'est ici qu'on découvre l'ampleur réelle du travail sur les séquences ANSI. Si
la traduction clavier ou le rendu s'avère bloquant, la roadmap change à cet endroit et nulle part
ailleurs.

---

> **Mise à jour après la phase 1** : l'émulateur de terminal (ex-phase 6) a été avancé ici, avant
> la phase 3 — voir `docs/adr/0001-rendu-ansi-et-clavier.md`. Conséquences sur ce qui suit : le
> scrollback est fourni par `pkg/screen` (émulateur) et non par un ring buffer maison ; `pkg/ansi`
> n'existe plus ; la phase 10 (ex-phase 6) se réduit à l'intégration multi-panneaux et aux
> cas limites.

## Phase 2 — Modèle de session

**But** : `pkg/session` autonome, testable sans TUI.

```go
type Session struct {
    ID         string
    Name       string
    Cmd        *exec.Cmd
    ptmx       *os.File
    scrollback *ringbuffer.Buffer  // borné, taille configurable
    Status     Status              // Running | Exited
    ExitCode   int
    CreatedAt  time.Time
    Cwd        string
}

type Manager struct {
    mu       sync.RWMutex
    sessions map[string]*Session
    order    []string
}
// New(name, shell) (*Session, error) ; Kill(id) ; List() []*Session ; Get(id)
```

Points clés :

- **Une goroutine de drain par session**, lancée à la création, vivante tant que le process tourne,
  **indépendante du `TaskManager`** : elle lit le pty en continu vers le scrollback. C'est ce qui
  garantit qu'aucune sortie n'est perdue pendant qu'une session n'est pas affichée.
- **Scrollback borné** — fourni par `pkg/screen` (l'émulateur en tient 10 000 lignes par défaut),
  plus de ring buffer à écrire. Ce n'est pas qu'une protection mémoire : le coût d'un redraw croît
  avec le tampon affiché, au point de figer l'UI (mesures dans l'ADR 0001).
- **Reaping** : `cmd.Wait()` dans la goroutine de drain → passage en `Exited` + code de sortie, sans
  laisser de zombie.
- **Kill propre** : `SIGHUP`/`SIGTERM` au *process group* (`syscall.Kill(-pgid, …)`, shell lancé avec
  `Setsid`), puis `SIGKILL` après timeout — sinon les enfants du shell survivent.
- **Arrêt global** : à la sortie de lazyshell, tuer toutes les sessions — le détach (daemon
  détenant les pty) est hors périmètre de la roadmap, c'est un autre projet.

**Critère de sortie** : tests unitaires `pkg/session` — création, écriture/lecture, kill, code de
sortie, borne du scrollback, absence de goroutine leak (`go test -race`).

**Risque** : moyen (gestion des process groups, portabilité macOS/Linux).

---

## Phase 3 — Layout, panels et navigation

**But** : l'UI à deux panneaux, avec des sessions réelles mais encore en lecture seule.

- `pkg/tasks` : port quasi verbatim du `TaskManager` de lazydocker (`NewTask` stoppe la tâche
  précédente, `NewTickerTask`).
- `pkg/gui/layout.go` : arbre `boxlayout` — `sessions` (gauche, largeur fixe ~30) + `output`
  (droite, poids restant) + barre de statut ; bascule `COLUMN`→`ROW` en mode portrait
  (`width <= 84 && height > 45`).
- `pkg/gui/sessions_panel.go` : liste (nom, statut, PID, cwd), navigation `j`/`k` + flèches,
  `OnSelect` → `QueueTask(render de la session)`.
- `pkg/gui/output.go` : la tâche de rendu vide le scrollback dans la vue puis suit le flux. Le
  `TaskManager` tue la tâche de *lecture* précédente — **jamais le process**.
- `pkg/gui/focus.go` : second manager gocui + hooks `onFocus`/`onFocusLost`, `view.Highlight` sur la
  vue courante (modèle lazydocker, pas de pile de contexts).
- `pkg/gui/keybindings.go` : liste plate de `Binding{ViewName, Key, Modifier, Handler, Description}`.
  Bindings : `n` nouvelle session, `x`/`d` kill (avec confirmation), `Tab` cycle focus, `q` quitter.

**Critère de sortie** : créer 3 sessions, alterner la sélection, voir la sortie de chacune ; une
session qui produit de la sortie pendant qu'elle est masquée n'a rien perdu au retour.

**Risque** : faible — c'est le terrain balisé par lazydocker.

---

## Phase 4 — Interactivité (mode pass-through) → **MVP**

**But** : lazyshell devient utilisable au quotidien pour des shells non-plein-écran.

- Intégration du travail de la phase 1 dans `pkg/gui/input.go` : quand `output` a le focus et que le
  mode pass-through est actif, chaque touche est traduite en octets et écrite dans `session.ptmx`.
- Indicateur de mode visible dans la barre de statut (`-- INSERT --` / bordure de couleur), sinon
  l'utilisateur ne sait plus si `q` quitte l'app ou part au shell.
- Préfixe d'échappement (décidé en phase 1) pour revenir à la navigation.
- Scroll dans le scrollback quand le pass-through est inactif (`PgUp`/`PgDn`, `Ctrl-U`/`Ctrl-D`),
  avec autoscroll réactivé au retour en bas.
- Propagation du resize : `layout()` → `pty.Setsize` de la session affichée (+ des autres, avec leur
  dernière taille connue).

**Critère de sortie** : dogfooding — utiliser lazyshell pour piloter 2-3 shells pendant une vraie
session de travail sans avoir à le tuer.

**Risque** : moyen — les cas limites clavier (touches composées, `Alt`, souris) sortent ici.

---

## Phase 5 — Configuration, thème, ergonomie

**But** : la finition qui rend le MVP présentable.

- `pkg/config` : YAML utilisateur (`~/.config/lazyshell/config.yml`) fusionné avec des defaults en
  dur — shell par défaut, taille de scrollback, largeur du panel, préfixe d'échappement, keybindings
  remappables.
- `pkg/gui/theme.go` : `activeBorderColor`, `inactiveBorderColor`, `selectedLineBgColor` → mappés en
  `gocui.Attribute` au démarrage (modèle lazydocker).
- Panneau d'aide `?` généré depuis les `Description` des bindings.
- Renommage de session (`r`), duplication, session dans un cwd choisi.
- Popup de confirmation pour le kill ; gestion d'erreur visible (pas de `panic` en plein écran).
- README avec un GIF de démo, instructions d'install (`go install`, Homebrew tap éventuel).

**Critère de sortie** : quelqu'un d'autre installe et utilise lazyshell depuis le README seul.

---

## Phase 6 — Config de projet : sessions déclaratives

**But** : `lazyshell` lancé dans un dossier qui contient un `lazyshell.yml` démarre tout seul les
sessions décrites dans ce fichier, chacune dans son cwd et avec sa commande. C'est ce qui fait
passer l'outil de « multiplexeur générique » à « lanceur d'environnement de dev d'un projet ».

**Découverte du fichier** (par ordre de priorité) :

1. `--config <fichier>` / `-f` en ligne de commande ;
2. `$LAZYSHELL_PROJECT_CONFIG` ;
3. `./lazyshell.yml` puis `./.lazyshell.yml` dans le répertoire courant.

Pas de remontée d'arborescence en première itération (voir « Ce qui reste ouvert »).

**Précédence de la configuration** : defaults en dur < `~/.config/lazyshell/config.yml` <
config de projet < variables d'environnement (`$LAZYSHELL_PREFIX`) < flags. Le mécanisme de merge
existe déjà (`config.Load` déserialise par-dessus `Default()`) : il suffit d'enchaîner deux
`yaml.Unmarshal` sur la même struct, dans cet ordre.

**Schéma du fichier** — le bloc `sessions` est le seul ajout au schéma existant ; les autres clés
sont celles de `pkg/config.Config`, surchargeables par projet :

```yaml
shell: /bin/zsh          # optionnel : surcharge la config utilisateur pour ce projet
sessions:
  - name: api
    cwd: ./services/api  # relatif au fichier de config, pas au cwd du process ; `~` étendu
    command: make dev
    env:
      PORT: "3000"
  - name: web
    cwd: ./web
    command: npm run dev
  - name: shell          # aucune commande : simple shell dans le cwd du projet
```

**Points à trancher / implémenter :**

- **Sémantique de `command`** : l'injecter dans le pty du shell interactif (façon `tmux send-keys`,
  le shell reste utilisable quand la commande se termine) plutôt que de l'`exec` à la place du
  shell (la session passerait `Exited` dès la fin de la commande). **Recommandation : injection**,
  à documenter — c'est le comportement attendu pour un `npm run dev` qu'on relance à la main.
- **Confiance** : un `lazyshell.yml` versionné dans un dépôt cloné exécute des commandes
  arbitraires au démarrage. Prévoir un garde-fou avant toute exécution automatique — prompt
  d'approbation par chemin, mémorisé (modèle `direnv allow`), plus un `--no-autostart` pour ouvrir
  l'UI sans rien lancer. **À ne pas repousser : c'est le seul point de cette phase qui est un vrai
  risque.**
- **Validation** : `name` unique et non vide, `cwd` existant, `shell` exécutable. Une entrée
  invalide n'empêche pas le démarrage des autres : la session concernée apparaît en erreur dans la
  liste, l'erreur est affichée dans la barre de statut (jamais de `panic`, jamais de sortie muette).
- **Support côté `pkg/session`** : `Manager.NewInDir(name, shell, cwd)` couvre déjà le cwd ; reste
  à ajouter l'environnement supplémentaire (`env`) et l'injection de la commande initiale.
- **Ordre et sélection** : sessions créées dans l'ordre du fichier, la première sélectionnée au
  démarrage.
- **`lazyshell init`** : génère un `lazyshell.yml` commenté dans le dossier courant, pour ne pas
  avoir à lire le README pour connaître le schéma.
- Documenter le format dans le README, à côté de la config utilisateur.

**Critère de sortie** : dans un dossier contenant un `lazyshell.yml` de 3 sessions, `lazyshell`
démarre les 3, chacune avec le bon cwd (`pwd` dans la session le confirme) et sa commande lancée ;
sans fichier, le comportement actuel est strictement inchangé. Tests : merge de configs et
validation dans `pkg/config`, autostart dans `pkg/app`, plus un test d'intégration bout-en-bout.

**Risque** : faible sur la mécanique (le chargement YAML et la création de session existent),
concentré sur le modèle de confiance.

---

## Phase 7 — Ergonomie multi-sessions

**But** : rendre supportable le régime que la phase 6 installe — 4 à 8 sessions ouvertes en
permanence, dont on n'en regarde qu'une. Quatre manques, tous petits, qui ne prennent leur sens
qu'ensemble.

- **Indicateur d'activité et de résultat dans la liste** : marqueur `●` sur une session masquée qui
  a produit de la sortie depuis la dernière fois qu'on l'a regardée (remis à zéro à la sélection),
  bell (`\a`) signalé distinctement, et `✓` / `✗ <code>` sur une session `Exited`. Le signal
  d'activité se prend dans la goroutine de drain de `pkg/session` (elle voit déjà passer chaque
  octet), pas dans la boucle de rendu.
- **Relance d'une session terminée** (`R`) : même nom, même cwd, même shell, même commande
  initiale. Suppose que la session conserve sa *spec* de création — c'est exactement la struct
  introduite par la phase 6 pour les sessions déclaratives, à extraire donc à ce moment-là et non
  à réinventer. Décider si la relance réutilise l'ID ou en crée un nouveau (recommandation : même
  ID, la session est « la même » du point de vue de l'utilisateur).
- **Saut direct par index** (`1`–`9` sélectionnent la n-ième session) et **zoom** (`z` : le
  panneau output prend tout l'écran, la liste disparaît ; même touche pour revenir). Le zoom est
  un cas particulier de `boxlayout` — un flag dans `layout()`, pas un second arbre.
- **Aides contextuelles permanentes, en anglais** : une ligne de rappel des raccourcis *pertinents
  ici et maintenant*, toujours visible, au lieu du seul popup `?` qui liste tout
  (`pkg/gui/help.go`). Le contexte, c'est le couple (vue ayant le focus, mode) :
  - `sessions` : `n new  x kill  r rename  R restart  z zoom  Tab output  ? help` ;
  - `output` en navigation : `Enter attach  PgUp/PgDn scroll  z zoom  Tab sessions` ;
  - `output` en pass-through : `<prefix> detach` et rien d'autre — c'est le seul moment où
    l'utilisateur peut se croire perdu ;
  - popups (confirm, prompt) : `Enter confirm  Esc cancel`.

  Implémentation : ajouter au `Binding` un libellé court et un ou plusieurs contextes, puis dériver
  la ligne d'aide *et* le popup `?` de la même source — jamais deux listes à maintenir en
  parallèle. Troncature propre par priorité quand la barre est plus étroite que le contenu (les
  raccourcis les moins utiles disparaissent en premier), et une ligne, pas deux : elle est prise
  sur la hauteur du panneau output.

  **Conséquence assumée : toute l'UI passe en anglais** (descriptions des bindings, titres de vues,
  messages d'erreur, popup `?`), aujourd'hui en français — un mélange des deux langues serait pire
  que l'un ou l'autre. À faire d'un bloc dans cette phase. La documentation (README, ROADMAP, ADR)
  reste en français.

**Critère de sortie** : lancer un projet de la phase 6, laisser tourner un build dans une session
masquée, voir le marqueur apparaître puis le code de sortie, la relancer avec `R` sans retaper la
commande, naviguer entre les sessions sans jamais utiliser `j`/`k` — et faire tout ça en ne lisant
que la barre d'aide, sans ouvrir `?` une seule fois.

**Risque** : faible. Attention seulement à ne pas faire du marqueur d'activité une source de redraw
permanent : il change d'état, il ne « clignote » pas.

---

## Phase 8 — Distribution et budget de performance

**But** : ce qui manque pour que le critère de sortie de la phase 5 (« quelqu'un d'autre installe
et utilise lazyshell depuis le README seul ») soit vraiment atteint.

- **Release automatisée** : `goreleaser`, binaires linux/macOS (amd64 + arm64), archives attachées
  au tag GitHub, checksums. `go install` documenté et vérifié sur une machine vierge, tap Homebrew
  si le besoin se confirme.
- **Version dans le binaire** : `--version` alimenté par `-ldflags`, affiché aussi dans le panneau
  d'aide — indispensable pour trier les rapports de bug.
- **Budget de redraw mesuré, pas commenté** : bench Go sur le rendu d'un écran avec 10 000 lignes
  de scrollback et N sessions actives, exécuté en CI avec un seuil qui casse le build en cas de
  régression. L'ADR 0001 documente déjà que ce coût peut figer l'UI ; le verrouiller par un test
  est le seul moyen que ça reste vrai après la phase 10.
- **GIF de démo** régénéré sur les fonctions des phases 6-7 (c'est là que l'outil devient
  démontrable en 15 secondes).

**Critère de sortie** : `brew install` ou `go install` sur une machine sans Go dev setup, puis
`lazyshell --version`, sans lire autre chose que le README.

**Risque** : nul techniquement, mais c'est la phase qu'on repousse indéfiniment si elle n'est pas
datée.

---

## Phase 9 — Recherche, copie, broadcast

**But** : les opérations qu'on quitte encore lazyshell pour faire dans un vrai terminal.

- **Recherche dans le scrollback** : `/` pour saisir un motif, `n`/`N` pour circuler, surlignage
  des occurrences, `Esc` pour sortir. Se fait sur le modèle de `pkg/screen` (les lignes, pas le
  flux d'octets).
- **Filtre de la liste de sessions** quand elle dépasse la hauteur du panneau — même champ de
  saisie, filtrage sur le nom et le cwd.
- **Copy-mode** : sélection de lignes au clavier, copie vers le presse-papier via OSC 52 (marche à
  travers SSH, contrairement à un appel à `xclip`/`pbcopy`), avec repli sur une commande externe
  configurable si le terminal ne supporte pas OSC 52.
- **Export** (`w`) : vider le scrollback d'une session dans un fichier, chemin proposé par défaut.
- **Broadcast** : marquer plusieurs sessions et leur envoyer la même saisie en pass-through.
  Fonction de niche, mais quasi gratuite une fois le routage clavier de la phase 4 en place — la
  garder derrière un indicateur très visible dans la barre de statut, une saisie envoyée à 6
  shells sans le savoir est un accident.

**Critère de sortie** : retrouver une stack trace dans 10 000 lignes de log et la coller dans un
éditeur, sans quitter lazyshell.

**Risque** : moyen — le copy-mode touche au rendu et interagit avec le zoom de la phase 7 et avec
l'émulateur de la phase 10.

---

## Phase 10 — Émulation de terminal complète (le vrai saut fonctionnel)

**But** : supporter `vim`, `htop`, `less` — ce qui sépare « joli visualiseur de sortie » de
« multiplexeur ».

- Intégrer un émulateur (`vt10x` / `charmbracelet/x/vt`) : le pty alimente une grille de cellules,
  la vue rend la grille à chaque frame.
- Gérer l'alternate screen, le positionnement du curseur, l'effacement, les attributs par cellule.
- Décision à prendre alors : rester sur `gocui.View` (qui n'est pas conçu pour un rendu cellule par
  cellule) ou descendre d'un niveau vers `tcell` pour le panel output uniquement.

**Risque** : élevé, périmètre large. À ne lancer qu'après un MVP utilisé et validé — c'est
exactement le genre de phase qui, faite trop tôt, enterre le projet.

---

## Séquencement et jalons

| Phase | Livrable | Jalon |
|---|---|---|
| 0 | squelette + CI | — |
| 1 | spike pty + ADR rendu/clavier | **go / no-go technique** |
| 2 | `pkg/session` testé | — |
| 3 | UI 2 panneaux, lecture seule | démo interne |
| 4 | pass-through interactif | **v0.1 — MVP dogfoodable** |
| 5 | config, thème, aide, README | **v0.2 — publiable** |
| 6 | `lazyshell.yml` de projet, sessions déclaratives | **v0.3** |
| 7 | activité, relance, saut par index, zoom, aides contextuelles | **v0.4** |
| 8 | goreleaser, `--version`, bench de redraw en CI | **v0.5 — installable par un tiers** |
| 9 | recherche, copy-mode, export, broadcast | v0.6 |
| 10 | émulation terminal complète | **v1.0** |

## Décisions déjà actées (ne pas re-débattre)

- gocui `jesseduffield` + `boxlayout` + `creack/pty`.
- Keybindings plats (lazydocker), **pas** le pattern controller de lazygit.
- `TaskManager` pour les goroutines d'*affichage* uniquement ; le cycle de vie des process shell est
  détenu par `session.Manager`, découplé.
- Structure de packages : celle du rapport (`pkg/{app,session,gui,tasks,config}`).

## Ce qui reste ouvert

1. Stratégie de rendu ANSI (tranchée en phase 1, revisitée en phase 10).
2. Préfixe d'échappement du mode pass-through.
3. Souris (sélection dans la liste, clic pour focus) : après le MVP.
4. Portée du support Windows : hors périmètre (pas de pty Unix) — à assumer explicitement.
5. Config de projet (phase 6) : remontée d'arborescence jusqu'à la racine du dépôt pour trouver
   `lazyshell.yml`, ou strictement le cwd ? Et un fichier de projet peut-il surcharger les
   keybindings et le thème, ou seulement `shell` / `sessions` ?
6. Modèle de confiance de l'auto-démarrage (phase 6) : approbation par chemin mémorisée, ou
   simple confirmation à chaque lancement ?
