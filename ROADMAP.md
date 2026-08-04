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
   en phase 6** (c'est-à-dire : la session expose « donne-moi l'état à afficher », pas « voici un
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
- **Scrollback borné** (ring buffer, ex. 10 000 lignes ou 1 Mo par session) — un `bytes.Buffer` non
  borné est une fuite mémoire garantie sur une session bavarde.
- **Reaping** : `cmd.Wait()` dans la goroutine de drain → passage en `Exited` + code de sortie, sans
  laisser de zombie.
- **Kill propre** : `SIGHUP`/`SIGTERM` au *process group* (`syscall.Kill(-pgid, …)`, shell lancé avec
  `Setsid`), puis `SIGKILL` après timeout — sinon les enfants du shell survivent.
- **Arrêt global** : à la sortie de lazyshell, tuer toutes les sessions (pas de détach en phase MVP,
  voir phase 7).

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

## Phase 6 — Émulation de terminal complète (le vrai saut fonctionnel)

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

## Phase 7 — Persistance et détach (optionnel, post-1.0)

**But** : la promesse tmux « ferme le terminal, les sessions survivent ».

Impose un changement d'architecture : lazyshell devient **client/serveur**, un daemon détenant les
pty et un TUI qui s'y connecte via socket Unix. C'est un projet en soi (protocole, gestion de
versions client/serveur, reconnexion, cycle de vie du daemon). À évaluer seulement si le besoin est
confirmé par l'usage.

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
| 6 | émulation terminal complète | **v1.0** |
| 7 | daemon + détach | v2.0 |

## Décisions déjà actées (ne pas re-débattre)

- gocui `jesseduffield` + `boxlayout` + `creack/pty`.
- Keybindings plats (lazydocker), **pas** le pattern controller de lazygit.
- `TaskManager` pour les goroutines d'*affichage* uniquement ; le cycle de vie des process shell est
  détenu par `session.Manager`, découplé.
- Structure de packages : celle du rapport (`pkg/{app,session,gui,tasks,config}`).

## Ce qui reste ouvert

1. Stratégie de rendu ANSI (tranchée en phase 1, revisitée en phase 6).
2. Préfixe d'échappement du mode pass-through.
3. Souris (sélection dans la liste, clic pour focus) : après le MVP.
4. Portée du support Windows : hors périmètre (pas de pty Unix) — à assumer explicitement.
