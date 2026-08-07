# Roadmap — lazyshell

Gestionnaire de sessions shell TUI (type tmux) en Go / gocui, dérivé de l'analyse
`RAPPORT_ANALYSE_LAZYGIT_LAZYDOCKER.md`.

Principe directeur : **chaque phase produit un binaire lançable et testable à la main**. On ne
construit jamais deux couches spéculatives d'affilée. Le risque technique majeur (pty + gocui qui se
disputent le terminal) est attaqué en phase 1, pas en phase 4.

**Légende d'avancement** — `[x]` fait · `[~]` en cours / partiel · `[ ]` à faire.
L'état est celui du code présent dans le dépôt, pas d'une intention.

| Phase | État |
|---|---|
| 0 · Socle du dépôt | **fait** |
| 1 · Spike pty | **fait** |
| 2 · Modèle de session | **fait** |
| 3 · Layout, panels, navigation | **fait** |
| 4 · Pass-through (MVP) | **fait** |
| 5 · Config, thème, ergonomie | **fait** |
| 6 · Config de projet | **fait** |
| 6.5 · Config utilisateur complète | **fait** |
| 7 · Ergonomie multi-sessions | **fait** |
| 8 · Distribution et budget de perf | **fait** |
| 9 · Recherche, copie, broadcast | **fait** |
| 10 · Émulation de terminal complète | **fait** (faite en avance, voir la phase) |
| 11 · Sessions d'agents IA | **partielle** — 11a et 11b faits (11b hors opencode), 11c à faire |

---

## Phase 0 — Socle du dépôt — **fait**

**But** : `go run ./cmd/lazyshell` affiche une fenêtre gocui vide qu'on peut quitter proprement.

- [x] `go mod init github.com/<user>/lazyshell` (Go 1.22+).
- [x] Dépendances initiales : `github.com/jesseduffield/gocui`,
  `github.com/jesseduffield/lazycore/pkg/boxlayout`, `github.com/creack/pty`, `gopkg.in/yaml.v3`.
  (Réalisé : gocui n'est disponible qu'en pseudo-version `master`, qui **exige Go 1.25** — le
  plancher « Go 1.22+ » ci-dessus est inatteignable. Les autres dépendances sont ajoutées à la
  phase qui les utilise, `go mod tidy` supprimant toute dépendance non importée.)
- [x] Arborescence minimale : `cmd/lazyshell/main.go`, `pkg/app`, `pkg/gui`, `pkg/session`, `pkg/tasks`.
- [x] `pkg/app` : bootstrap (construit le `SessionManager`, construit le `Gui`, appelle `gui.Run()`).
- [x] `pkg/gui/gui.go` : `gocui.NewGui`, `SetManager`, `MainLoop`, binding `q` / `Ctrl-C` → `ErrQuit`,
  restauration du terminal via `defer g.Close()`.
- [x] Outillage : `Makefile` (`build`, `run`, `test`, `lint`), `.gitignore`, `golangci-lint`, CI GitHub
  Actions (build + vet + test sur linux/macos).

**Critère de sortie** : ouverture/fermeture sans laisser le terminal dans un état cassé (pas
d'écho perdu, pas de curseur invisible).

**Risque** : aucun. Phase mécanique.

---

## Phase 1 — Spike pty (la phase qui décide de tout) — **fait**

**But** : valider *avant* d'investir dans l'UI que gocui et un pty interactif cohabitent. C'est le
seul point sans précédent dans lazygit/lazydocker.

Un binaire jetable `cmd/spike-pty` : une seule vue plein écran, un seul `bash` derrière un pty.

- [x] `pty.Start(exec.Command(shell))` → `ptmx *os.File`.
- [x] Goroutine de lecture : `io.Copy(view, ptmx)` (la `gocui.View` est un `io.Writer` thread-safe).
  (Réalisé autrement : remplacé par l'alimentation de l'émulateur, voir la décision 1 ci-dessous.)
- [x] `goEvery(30ms, reRender)` : si `view.IsTainted()`, `g.Update(noop)` pour déclencher le redraw.
- [x] Clavier → `ptmx.Write()` : c'est **le** point dur. gocui livre des `gocui.Key`/rune, pas des
  octets bruts. Il faut une table de traduction (touches fléchées → séquences CSI `ESC[A`…,
  `Ctrl-<x>` → octet de contrôle, `Backspace` → `0x7f`, `Enter` → `\r`). → `pkg/keys`.
- [x] `pty.Setsize` avec les dimensions réelles de la vue, recalculées dans `layout()`.

**Questions à trancher ici, à documenter dans un ADR :** toutes tranchées dans
`docs/adr/0001-rendu-ansi-et-clavier.md`.

1. [x] **Rendu des séquences ANSI.** gocui interprète un sous-ensemble de SGR (couleurs, gras), mais pas
   les séquences de positionnement de curseur (`ESC[H`, `ESC[2J`, mode alternate screen). Conséquence
   directe : `ls`, `git log`, un prompt coloré passent ; `vim`, `htop`, `less` **ne passeront pas**
   tels quels. Trois options, à décider maintenant :
   - (a) MVP « line-oriented » assumé : on documente que les applis plein écran ne sont pas
     supportées ;
   - (b) émulateur de terminal en amont (`github.com/hinshun/vt10x` ou `charmbracelet/x/vt`) : on
     maintient une grille de cellules, et on rend cette grille dans la vue à chaque frame au lieu
     d'`io.Copy` — coût réel, mais c'est la seule voie vers un vrai tmux ;
   - (c) filtrage/strip des séquences non supportées pour éviter l'affichage corrompu.
   **Recommandation initiale : (a) pour les phases 1-4, avec l'interface de rendu conçue pour
   permettre (b) en phase 10.** *Tranché autrement après essais réels : (b) tout de suite — (a) et
   (c) ont été essayées et abandonnées, un prompt thémé se redessine en place et est intraduisible
   sans grille de cellules.*
2. [x] **Qui possède le terminal** quand une session est en pass-through : gocui garde toujours le
   contrôle (on ne fait *pas* de `Suspend`/`Resume`, sinon on perd le multiplexage).
3. [x] **Sortie du mode pass-through** : un préfixe d'échappement à la tmux (`Ctrl-A`, `Ctrl-B` ou
   `Ctrl-Space`) car `Tab` et `Esc` doivent partir au shell. → `Ctrl-B`, remappable.

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

## Phase 2 — Modèle de session — **fait**

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

- [x] **Une goroutine de drain par session**, lancée à la création, vivante tant que le process tourne,
  **indépendante du `TaskManager`** : elle lit le pty en continu vers le scrollback. C'est ce qui
  garantit qu'aucune sortie n'est perdue pendant qu'une session n'est pas affichée.
- [x] **Scrollback borné** — fourni par `pkg/screen` (l'émulateur en tient 10 000 lignes par défaut),
  plus de ring buffer à écrire. Ce n'est pas qu'une protection mémoire : le coût d'un redraw croît
  avec le tampon affiché, au point de figer l'UI (mesures dans l'ADR 0001).
- [x] **Reaping** : `cmd.Wait()` dans la goroutine de drain → passage en `Exited` + code de sortie, sans
  laisser de zombie.
- [x] **Kill propre** : `SIGHUP`/`SIGTERM` au *process group* (`syscall.Kill(-pgid, …)`, shell lancé avec
  `Setsid`), puis `SIGKILL` après timeout — sinon les enfants du shell survivent.
- [x] **Arrêt global** : à la sortie de lazyshell, tuer toutes les sessions — le détach (daemon
  détenant les pty) est hors périmètre de la roadmap, c'est un autre projet.

**Critère de sortie** : tests unitaires `pkg/session` — création, écriture/lecture, kill, code de
sortie, borne du scrollback, absence de goroutine leak (`go test -race`).

**Risque** : moyen (gestion des process groups, portabilité macOS/Linux).

---

## Phase 3 — Layout, panels et navigation — **fait**

**But** : l'UI à deux panneaux, avec des sessions réelles mais encore en lecture seule.

- [x] `pkg/tasks` : port quasi verbatim du `TaskManager` de lazydocker (`NewTask` stoppe la tâche
  précédente, `NewTickerTask`).
- [x] `pkg/gui/layout.go` : arbre `boxlayout` — `sessions` (gauche, largeur fixe ~30) + `output`
  (droite, poids restant) + barre de statut ; bascule `COLUMN`→`ROW` en mode portrait
  (`width <= 84 && height > 45`).
- [x] `pkg/gui/sessions_panel.go` : liste (nom, statut, PID, cwd), navigation `j`/`k` + flèches,
  `OnSelect` → `QueueTask(render de la session)`.
- [x] `pkg/gui/output.go` : la tâche de rendu vide le scrollback dans la vue puis suit le flux. Le
  `TaskManager` tue la tâche de *lecture* précédente — **jamais le process**.
- [x] `pkg/gui/focus.go` : second manager gocui + hooks `onFocus`/`onFocusLost`, `view.Highlight` sur la
  vue courante (modèle lazydocker, pas de pile de contexts).
- [x] `pkg/gui/keybindings.go` : liste plate de `Binding{ViewName, Key, Modifier, Handler, Description}`.
  Bindings : `n` nouvelle session, `x`/`d` kill (avec confirmation), `Tab` cycle focus, `q` quitter.

**Critère de sortie** : créer 3 sessions, alterner la sélection, voir la sortie de chacune ; une
session qui produit de la sortie pendant qu'elle est masquée n'a rien perdu au retour.

**Risque** : faible — c'est le terrain balisé par lazydocker.

---

## Phase 4 — Interactivité (mode pass-through) → **MVP** — **fait**

**But** : lazyshell devient utilisable au quotidien pour des shells non-plein-écran.

- [x] Intégration du travail de la phase 1 dans `pkg/gui/input.go` : quand `output` a le focus et que le
  mode pass-through est actif, chaque touche est traduite en octets et écrite dans `session.ptmx`.
- [x] Indicateur de mode visible dans la barre de statut (`-- INSERT --` / bordure de couleur), sinon
  l'utilisateur ne sait plus si `q` quitte l'app ou part au shell.
- [x] Préfixe d'échappement (décidé en phase 1) pour revenir à la navigation.
- [x] Scroll dans le scrollback quand le pass-through est inactif (`PgUp`/`PgDn`, `Ctrl-U`/`Ctrl-D`),
  avec autoscroll réactivé au retour en bas (décalage ramené à 0 = vue vivante).
- [x] Propagation du resize : `layout()` → `pty.Setsize` de la session affichée (+ des autres, avec leur
  dernière taille connue).

**Critère de sortie** : dogfooding — utiliser lazyshell pour piloter 2-3 shells pendant une vraie
session de travail sans avoir à le tuer.

**Risque** : moyen — les cas limites clavier (touches composées, `Alt`, souris) sortent ici.

---

## Phase 5 — Configuration, thème, ergonomie — **fait**

**But** : la finition qui rend le MVP présentable.

- [x] `pkg/config` : YAML utilisateur (`~/.config/lazyshell/config.yml`) fusionné avec des defaults en
  dur — shell par défaut, taille de scrollback, largeur du panel, préfixe d'échappement, keybindings
  remappables.
- [x] `pkg/gui/theme.go` : `activeBorderColor`, `inactiveBorderColor`, `selectedLineBgColor` → mappés en
  `gocui.Attribute` au démarrage (modèle lazydocker).
- [x] Panneau d'aide `?` généré depuis les `Description` des bindings.
- [x] Renommage de session (`r`), duplication, session dans un cwd choisi.
- [x] Popup de confirmation pour le kill ; gestion d'erreur visible (pas de `panic` en plein écran).
- [x] README avec instructions d'install (`go install`), **et** `docs/demo.gif` généré et
  intégré (`vhs` installé après coup, `make demo` corrigé — voir la phase 8). Le tap Homebrew
  reste repoussé, en phase 8.

**Critère de sortie — atteint, texte et démo visuelle.** Quelqu'un d'autre installe et utilise
lazyshell depuis le README seul.

---

## Phase 6 — Config de projet : sessions déclaratives — **fait**

**But** : `lazyshell` lancé dans un dossier qui contient un `lazyshell.yml` démarre tout seul les
sessions décrites dans ce fichier, chacune dans son cwd et avec sa commande. C'est ce qui fait
passer l'outil de « multiplexeur générique » à « lanceur d'environnement de dev d'un projet ».

**Découverte du fichier** (par ordre de priorité) — `pkg/config/project.go`, `ProjectPath` :

1. [x] `--config-file <fichier>` / `-f` en ligne de commande ;
2. [x] `$LAZYSHELL_PROJECT_CONFIG` ;
3. [x] `./lazyshell.yml` puis `./.lazyshell.yml` dans le répertoire courant.

Pas de remontée d'arborescence : **décision prise**, c'est le cwd strict. Un chemin explicite
(flag ou env) est retourné même s'il n'existe pas — demander un fichier précis et n'obtenir que du
silence est pire que l'erreur.

- [x] **Précédence de la configuration** : defaults en dur < `~/.config/lazyshell/config.yml` <
  config de projet < variables d'environnement (`$LAZYSHELL_PREFIX`) < flags. Réalisé par
  `Config.MergeProject`, **pas** par un second `yaml.Unmarshal` sur la même struct comme envisagé
  ici : voir la décision de portée ci-dessous.

**Schéma du fichier** — le bloc `sessions` est le seul ajout au schéma existant. **Correction par
rapport au plan initial** : les autres clés de `pkg/config.Config` ne sont **pas** surchargeables
par projet. Un fichier de projet ne peut écrire que `shell` et `sessions` (`ProjectConfig`, dont la
struct *est* la liste blanche) ; `theme`, `keybindings` et `prefix_key` restent la propriété de
l'utilisateur, sinon un dépôt cloné remappe son clavier. Les clés hors liste sont ignorées avec un
avertissement sur stderr.

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

**Points tranchés / implémentés :**

- [x] **Sémantique de `command`** : **injection** retenue. `Manager.NewWithOptions` écrit
  `command + "\n"` dans le pty juste après le démarrage ; la discipline de ligne du pty la garde en
  tampon jusqu'à ce que le shell la lise, donc pas besoin d'attendre le premier prompt. Le shell
  reste sous la commande, la session ne passe pas `Exited` à la fin d'un `npm run dev`. Vérifié par
  `TestNewWithOptionsInjectsTheCommand`, qui teste les deux moitiés : la commande tourne, *et* le
  shell répond encore après.
- [x] **Confiance** : modèle `direnv`. `pkg/config/trust.go` mémorise `chemin absolu → sha256 du
  contenu` dans `trust.yml` à côté de la config utilisateur ; toute modification du fichier
  redemande l'approbation. Le prompt a lieu dans `pkg/app` **avant** que gocui prenne le terminal.
  `lazyshell allow [fichier]` approuve sans rien lancer, `--no-autostart` ouvre l'UI sans démarrer.
  **stdin non-tty ⇒ refus**, jamais de blocage — et le test doit être un vrai `term.IsTerminal`
  (ioctl), pas un `Stat`/`ModeCharDevice` : `/dev/null` est aussi un périphérique caractère, et la
  version naïve affichait le prompt à `lazyshell < /dev/null`.
- [x] **Validation** : `name` unique et non vide, `cwd` résolu et existant. Une entrée invalide est
  écartée, les autres démarrent, et les erreurs sont concaténées dans la barre de statut via
  `Gui.SetStartupError` (jamais de `panic`, jamais de sortie muette). `shell` exécutable n'est pas
  vérifié en amont : l'échec de `pty.Start` le dit déjà, avec un meilleur message.
- [x] **Support côté `pkg/session`** : `Manager.NewWithOptions(Options{Name, Shell, Cwd, Env,
  Command})`. `New` et `NewInDir` en sont devenus des wrappers — aucun appelant existant touché.
- [x] **Ordre et sélection** : sessions créées dans l'ordre du fichier. La sélection initiale a
  demandé un raccord dans `Gui.Run` : rien n'appelait `onSelectionChanged` avant la première
  touche, donc le panneau output serait resté vide alors que trois sessions tournaient.
- [x] **`lazyshell init`** : `pkg/app/init.go`, ouverture en `O_EXCL` (n'écrase jamais un fichier
  existant). Le gabarit est testé — un exemple que la validation refuserait serait pire que rien.
- [x] Format documenté dans le README, à côté de la config utilisateur.

**Critère de sortie — atteint.** Vérifié à la main en pilotant le binaire dans un pty : trois
sessions déclarées démarrent dans l'ordre du fichier, la première est sélectionnée et son output
s'affiche sans toucher une touche, `pwd` donne le bon cwd, la commande a tourné et le shell est
toujours là après. Sans fichier, le comportement est inchangé (`TestNoProjectFileStartsNothing`).
Tests : `pkg/config/project_test.go` et `trust_test.go`, `pkg/session/options_test.go`,
`pkg/app/autostart_test.go` (bout-en-bout, vrais pty) et `init_test.go`.

**Risque** : confirmé faible sur la mécanique. Le seul défaut trouvé était bien dans le modèle de
confiance, et il n'est pas sorti des tests unitaires mais du premier lancement à la main.

---

## Phase 6.5 — Configuration utilisateur complète, générée et documentée — **fait**

**But** : que tout ce qui se règle dans lazyshell se règle depuis
`~/.config/lazyshell/config.yml`, que le fichier se génère au lieu de se deviner, et qu'une
erreur dedans se voie. Intercalée avant la phase 7 parce que la bascule de langue de cette
phase doit se piloter par une option, et parce que le socle de config n'était pas aussi
complet qu'il en avait l'air.

**Ce qui existait déjà** (phases 3 à 6, souvent oublié) : `config.Path()` et sa précédence
`$LAZYSHELL_CONFIG` > `$XDG_CONFIG_HOME` > `~/.config`, `config.Load` fusionnant sur
`Default()`, et les clés `shell`, `scrollback_size`, `sessions_panel_width`, `prefix_key`,
`keybindings` (10 actions), `theme`.

**Ce que la phase ajoute :**

- [x] **Onze options de plus**, chacune câblée à la constante qu'elle remplace, validée et
  documentée : `language` (`fr`/`en`, lue et validée, appliquée en phase 7),
  `term`, `refresh_interval_ms`, `kill_timeout_ms`, `sessions_panel_height`,
  `portrait_max_width`, `portrait_min_height`, `markers.bell`, `markers.alt_screen`,
  `scroll.page_lines`, `scroll.half_page_divisor`. Les constantes restent dans leur
  package comme défaut — la config les surcharge, elle ne les remplace pas — et
  `pkg/config` n'acquiert toujours aucune dépendance vers `gocui` ou `vt`.
- [x] **`lazyshell config init`** (`pkg/app/config_cmd.go`) : écrit un fichier entièrement
  commenté, dossier parent créé, ouverture en `O_EXCL` donc jamais d'écrasement. Le
  gabarit est testé : il doit charger sans un seul avertissement *et* donner exactement
  les défauts, sinon on livre un exemple qui reconfigure ceux qui le copient.
- [x] **`lazyshell config show`** : la configuration réellement appliquée, sources en
  commentaire. C'est la réponse à « pourquoi ma clé ne prend pas ». Elle imprime les
  valeurs *effectives*, pas celles du fichier : `gui.Effective` remplit le thème et les
  touches dont les vrais défauts vivent dans `pkg/gui`, et rend en syntaxe `gocui.Parse`
  (`Ctrl+Q`, pas le `Ctrl-Q` d'affichage) puisque la sortie se relit comme un fichier de
  config.
- [x] **Fin du silence.** Trois trous bouchés, tous du même genre — une valeur inutilisable
  dégradait proprement, sans jamais le dire : `unknownKeys` (qui n'existait que pour les
  fichiers de projet) s'applique désormais à la config utilisateur, `Config.Validate`
  ramène toute valeur hors bornes à son défaut en le signalant, et `gui.ValidateConfig`
  rapporte les touches illisibles, les ids d'action inexistants (`new_sesion:`) et les
  couleurs inconnues. **Rien n'est fatal** : tout est corrigé, dit sur stderr avant que
  gocui prenne le terminal, puis résumé dans la barre de statut.
- [x] **README** : tableau de référence (clé, type, défaut, effet) plus l'exemple complet.
- [x] **Test de synchronisation doc ↔ code** (`pkg/config/doc_test.go`) : les tags `yaml`
  de `Config` sont comparés par réflexion aux clés du tableau *et* de l'exemple, dans les
  deux sens. Une option ajoutée sans sa ligne de README casse le build. `pkg/gui` complète
  en vérifiant que les touches et couleurs documentées sont bien celles qui s'appliquent —
  leurs défauts vivent là et `pkg/config` ne peut pas les connaître.

**Trouvé au passage** : le test de synchronisation a fait remonter, dès sa première
exécution, que `selected_bg_color: blue` et `pass_through_border_color: red` (documentés
et codés en dur depuis la phase 3) ne donnaient pas le bleu et le rouge ordinaires d'un
terminal. `gocui.GetColor` suit la table W3C/CSS, où `blue` vaut `#0000FF` — le bleu *vif*
d'un terminal, pas l'ordinaire, qui s'y appelle `navy`. Une divergence purement silencieuse
: le nom résolvait, juste vers la mauvaise nuance.

Corrigé à la racine plutôt qu'en changeant seulement la doc : `ansiColorAliases`
(`pkg/gui/theme.go`) couvre maintenant toute la table ANSI (les 8 couleurs de base et
leurs variantes `bright*`), résolue en priorité sur la table W3C. `blue`/`red` désignent de
nouveau le bleu et le rouge ordinaires du terminal ; le bleu CSS reste accessible sous
`navy`, le bleu vif sous `brightblue`. Verrouillé par
`TestAnsiAliasesResolveToTerminalColors` (`pkg/gui/theme_alias_test.go`).

**Critère de sortie — atteint.** Vérifié à la main : `config init` crée le fichier puis
refuse de l'écraser ; un fichier contenant une clé mal orthographiée, une langue inconnue,
un intervalle nul, une action inexistante et une touche illisible produit cinq messages
distincts, démarre quand même, et `config show` affiche les valeurs corrigées.

**Reste ouvert** : `language` ne fait rien encore — l'interface est en français, et c'est
la phase 7 qui la traduit. Le champ existe maintenant pour que cette phase soit un travail
de traduction et pas aussi un travail de configuration.

---

## Phase 7 — Ergonomie multi-sessions — **fait**

**But** : rendre supportable le régime que la phase 6 installe — 4 à 8 sessions ouvertes en
permanence, dont on n'en regarde qu'une. Quatre manques, tous petits, qui ne prennent leur sens
qu'ensemble.

- [x] **Indicateur d'activité et de résultat dans la liste** : marqueur `●` sur une session masquée qui
  a produit de la sortie depuis la dernière fois qu'on l'a regardée (remis à zéro à la sélection),
  bell (`\a`) signalé distinctement, et `✓` / `✗ <code>` sur une session `Exited`. Le signal
  d'activité se prend dans la goroutine de drain de `pkg/session` (elle voit déjà passer chaque
  octet), pas dans la boucle de rendu.
  (*La cloche et la gouttière de marqueurs existent déjà depuis la phase 10 — `bellMarker` /
  `altScreenMarker` dans `pkg/gui/sessions_panel.go` ; il reste le marqueur d'activité et le
  résultat de sortie, à y ajouter plutôt qu'à côté.*)
- [x] **Relance d'une session terminée** (`R`) : même nom, même cwd, même shell, même commande
  initiale. Suppose que la session conserve sa *spec* de création — c'est exactement la struct
  introduite par la phase 6 pour les sessions déclaratives, à extraire donc à ce moment-là et non
  à réinventer. Décider si la relance réutilise l'ID ou en crée un nouveau (recommandation : même
  ID, la session est « la même » du point de vue de l'utilisateur).
- [x] **Saut direct par index** (`1`–`9` sélectionnent la n-ième session) et **zoom** (`z` : le
  panneau output prend tout l'écran, la liste disparaît ; même touche pour revenir). Le zoom est
  un cas particulier de `boxlayout` — un flag dans `layout()`, pas un second arbre.
- [x] **Aides contextuelles permanentes, en anglais** : une ligne de rappel des raccourcis *pertinents
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

  **Conséquence assumée : toute l'UI devient traduisible, fr et en** (descriptions des bindings,
  titres de vues, messages d'erreur, popup `?`, sorties CLI de `pkg/app`), aujourd'hui en français
  en dur — un mélange des deux langues serait pire que l'un ou l'autre, donc c'est à faire d'un
  bloc dans cette phase. La documentation (README, ROADMAP, ADR) reste en français.

  Le choix ne se refait pas ici : `language` existe depuis la phase 6.5, elle est lue, validée et
  documentée. Il ne reste qu'à extraire les chaînes et à les brancher dessus.

**Critère de sortie** : lancer un projet de la phase 6, laisser tourner un build dans une session
masquée, voir le marqueur apparaître puis le code de sortie, la relancer avec `R` sans retaper la
commande, naviguer entre les sessions sans jamais utiliser `j`/`k` — et faire tout ça en ne lisant
que la barre d'aide, sans ouvrir `?` une seule fois.

**Risque** : faible. Attention seulement à ne pas faire du marqueur d'activité une source de redraw
permanent : il change d'état, il ne « clignote » pas.

---

## Phase 8 — Distribution et budget de performance — **fait**

**But** : ce qui manque pour que le critère de sortie de la phase 5 (« quelqu'un d'autre installe
et utilise lazyshell depuis le README seul ») soit vraiment atteint.

- [x] **Release automatisée** : `.goreleaser.yml` (linux/macOS, amd64 + arm64, ldflags
  `version.Version`, archives `tar.gz` + `checksums.txt`) et `.github/workflows/release.yml`
  déclenché sur un tag `v*`. Pas de section Homebrew : différée avec l'absence de `LICENSE`, comme
  prévu ici (« si le besoin se confirme »). `go install` déjà documenté dans le README ; l'install
  binaire précompilé y est ajoutée à côté.
- [x] **Version dans le binaire** : `--version` alimenté par `-ldflags`, affiché aussi dans le panneau
  d'aide — indispensable pour trier les rapports de bug.
- [x] **Budget de redraw mesuré, pas commenté** : `pkg/screen/perf_test.go` et `pkg/gui/perf_test.go`
  (tag `!race`, exclus du `go test -race` déjà en CI) enveloppent les benchs existants dans
  `testing.Benchmark` et font échouer le test si le `ns/op` dépasse un seuil fixé avec une large
  marge sur une mesure locale — nouvelle étape `go test -run TestPerfBudget ./...` dans
  `ci.yml`. `TestIdleSessionDoesNotRepaint` (phase 10) était déjà gaté et tourne déjà dans le job
  existant.
- [x] **GIF de démo** : `docs/demo.gif` généré et intégré au README, démontrant le saut par index,
  le zoom et la relance `R` des phases 6-7 — recouvre le point resté ouvert de la phase 5.
  `vhs` n'avait jamais tourné contre `docs/demo.tape` (« pas disponible dans l'environnement qui a
  écrit le script ») : une fois installé, le script échouait — un `Type` multiligne délimité par
  des backticks, jamais un format que `vhs` reconnaît (seules les chaînes `"..."` sur une seule
  ligne existent dans sa grammaire), que le validateur n'avait donc jamais pu attraper avant.
  Corrigé en une seule ligne `printf '...\n...\n...'` : les `\n` sont tapés comme deux caractères
  littéraux, décodés par `printf` lui-même une fois dans le shell enregistré, pas par `vhs`.

**Critère de sortie — atteint.** `brew install` ou `go install` sur une machine sans Go dev setup,
puis `lazyshell --version`, sans lire autre chose que le README.

**Risque** : nul techniquement, mais c'est la phase qu'on repousse indéfiniment si elle n'est pas
datée.

---

## Phase 9 — Recherche, copie, broadcast — **fait**

**But** : les opérations qu'on quitte encore lazyshell pour faire dans un vrai terminal.

- [x] **Recherche dans le scrollback** : `/` pour saisir un motif, `n`/`N` pour circuler, surlignage
  des occurrences, `Esc` pour sortir. Se fait sur le modèle de `pkg/screen` (les lignes, pas le
  flux d'octets).
- [x] **Filtre de la liste de sessions** quand elle dépasse la hauteur du panneau — même champ de
  saisie, filtrage sur le nom et le cwd. `pkg/gui/filter.go` : `/` sur le panneau sessions ouvre le
  même popup que la recherche (`showPrompt`), `Esc` l'efface. `filteredSessions()` est la seule
  source que `selectedSession`, `selectionMoved`, `selectIndex` et le rendu de la liste consultent
  désormais — jamais `gui.sessions.List()` directement — pour que `1`-`9` adresse toujours ce qui
  est réellement affiché. La sélection change de repère (index → ID) au moment où le filtre change
  pour ne pas sauter sur une autre session à chaque caractère tapé ; la création d'une session
  efface un filtre actif, sinon elle naîtrait cachée.
- [x] **Copy-mode** : sélection de lignes au clavier, copie vers le presse-papier via OSC 52 (marche à
  travers SSH, contrairement à un appel à `xclip`/`pbcopy`), avec repli sur une commande externe
  configurable si le terminal ne supporte pas OSC 52. `v` démarre une sélection d'une ligne (le
  haut de la fenêtre visible), `j`/`k`/flèches l'étendent, un second `v` ou `y` copie et sort, `Esc`
  annule. `pkg/screen` gagne `RenderAtSelection` (surlignage par plage de lignes, pendant du
  surlignage par motif de la recherche) et `TextRange` (texte brut sur une plage — réutilisé tel
  quel par l'export). `pkg/config`'s `clipboard.fallback_command` est un interrupteur manuel : rien
  ne permet de savoir si le terminal a vraiment accepté la séquence OSC 52, donc vide = OSC 52
  seul, renseigné = cette commande à la place (le texte sur son stdin), jamais les deux. Désactivé
  sur l'alternate screen, comme le défilement.
- [x] **Export** (`w`) : vider le scrollback d'une session dans un fichier, chemin proposé par défaut.
  `pkg/gui/export.go` : prompt pré-rempli avec `<cwd de la session>/<nom>-<horodatage>.log`,
  réutilise `Screen.TextRange(0, math.MaxInt)` (déjà écrit pour le copy-mode) sans nouvelle méthode
  `Screen`. Écrasement volontaire, sans `O_EXCL` — contrairement aux gabarits de config, un export
  est une capture jetable qu'on redemande au même endroit. Premier message de succès de l'appli :
  `Gui.lastInfo`, pendant positif de `lastError` (chacun efface l'autre).
- [x] **Broadcast** : marquer plusieurs sessions et leur envoyer la même saisie en pass-through.
  `pkg/gui/broadcast.go` : `b` marque/démarque la session sélectionnée ; la diffusion s'arme
  d'elle-même à partir de 2 marques (une seule marque n'a personne à qui diffuser), et s'éteint
  d'elle-même en dessous. `dispatchKey` (`input.go`) traduit séparément pour chaque session
  ciblée — DECCKM (mode curseur applicatif) est un état par session, deux cibles peuvent avoir
  besoin de deux encodages différents pour la même touche. Indicateur `⚠ DIFFUSION → N sessions`
  préfixé à *tout* le reste de la barre de statut (y compris pendant le pass-through, le moment où
  il compte le plus) plutôt qu'une simple entrée de priorité parmi d'autres — une saisie envoyée à
  plusieurs shells sans le savoir est justement l'accident à éviter. Marqueur `+` dans une
  quatrième colonne de gouttière (`markers.broadcast`, config).

**Critère de sortie** : retrouver une stack trace dans 10 000 lignes de log et la coller dans un
éditeur, sans quitter lazyshell.

**Risque** : moyen — le copy-mode touche au rendu et interagit avec le zoom de la phase 7 et avec
l'émulateur de la phase 10.

---

## Phase 10 — Émulation de terminal complète (le vrai saut fonctionnel) — **fait**

**But** : supporter `vim`, `htop`, `less` — ce qui sépare « joli visualiseur de sortie » de
« multiplexeur ».

Faite en avance sur le séquencement, pour une raison qui n'était pas visible en la planifiant :
son premier point corrige un défaut de la **v0.2 déjà publiée**, pas une fonctionnalité future.
L'émulateur ayant été avancé en phase 1 (ADR 0001), il ne restait ici que l'intégration dans l'UI
à deux panneaux et les cas limites. Détail et mesures dans `docs/adr/0002-rendu-multi-panneaux.md`.

- [x] **Rendu** — le défaut : `pkg/gui` initialisait gocui en `OutputNormal`, alors que le spike de la
  phase 1 avait établi qu'`OutputTrue` est indispensable. En dessous, gocui rejette les formes SGR
  256 couleurs et truecolor et en **imprime le corps** : tout prompt thémé, tout colorscheme `vim`,
  toute barre `htop` s'affichait avec des `[38;5;2m` en clair. Corrigé, avec `InputEsc` (sans lui,
  `Esc` — la touche centrale de `vim` — n'arrive pas de façon fiable).
- [x] **Curseur** : `pkg/screen` expose position et visibilité (cette dernière via les callbacks de
  `vt`, qui n'en donne pas de getter) ; le panneau output le dessine quand il a le focus, en
  pass-through, sur l'écran vivant, et si l'application ne l'a pas caché.
- [x] **Alternate screen** : signalé (`[ALT]` dans la barre de statut, `#` dans la gouttière de la
  liste) et défilement neutralisé — mais **pas** de bascule de mode automatique : un changement de
  mode non demandé rendrait ambigu ce que fait `q`.
- [x] **Clavier** : mode curseur applicatif (DECCKM) suivi par l'émulateur et honoré par
  `keys.TranslateWithMode` (formes SS3), sans quoi les flèches tapent une lettre dans `less`.
- [x] **`gocui.View` ou `tcell` ?** Tranché par la mesure, pas par le raisonnement : le rendu d'une
  frame coûte 0,2 % à 1,7 % du tick de 30 ms selon la géométrie, et un seul panneau est rendu à la
  fois. On reste sur `gocui.View`. Le vrai coût était ailleurs : les deux panneaux appelaient
  `g.Update` à chaque tick même sans rien à afficher, soit **60 repeints plein écran par seconde
  au repos**, ramenés à **0**.
- [x] **Cas limites** : titre OSC affiché dans la liste (souvent la commande en cours), cloche à
  verrou pour une session qui a sonné pendant qu'elle était masquée, et erreur visible quand on
  tape dans une session dont le shell est mort.

**Reste ouvert, hors périmètre assumé** (voir ADR 0002) : la souris (gocui confond ses boutons avec
les Shift-flèches), les protocoles clavier étendus (Kitty, CSI u), le collage entre crochets.

**Risque** : il était annoncé élevé et à large périmètre ; l'avoir amputé de l'émulateur dès la
phase 1 l'a ramené à une phase d'intégration.

---

## Phase 11 — Sessions d'agents IA — **partielle (11a et 11b faits)**

**But** : traiter les CLI d'agents (`claude`, `codex`, `opencode`…) comme des locataires de session
de première classe — savoir dans quel état ils sont, le montrer, et prévenir quand ils *attendent*.
Aucun modèle, aucun prompt, aucun chat dans lazyshell : uniquement de l'observation.

Analyse complète, état de l'art (`herdr`, `ccmux`, `CodeAgentSwarm`, `claude-squad`, `ccusage`) et
justification des choix ci-dessous : `RAPPORT_ANALYSE_INTEGRATION_AGENTS_IA.md`.

**Pourquoi ici et pas plus tôt** : la phase 6 apporte `SessionSpec{Command, Env}` — donc
`command: claude` déclaratif *et* l'injection d'une variable d'environnement par session, qui est ce
qui rend la corrélation hook → session triviale. La phase 7 apporte la gouttière de marqueurs
enrichie, le saut par index et la barre d'aide contextuelle. Faite avant, cette phase les
réinventerait en double.

**Le manque visé, en une phrase** : tmux ne distingue pas « il y a eu de l'activité » de « il
t'attend ». Le marqueur d'activité de la phase 7 est vrai dans les deux cas.

### 11a — Détection sans configuration — **fait**

- [x] `pkg/agent` : un état à quatre valeurs par session — `idle` / `working` / `blocked` / `done`
  (le modèle de `herdr`, qui s'est révélé être le bon découpage), plus `StateNone` pour « ce n'est
  pas une session d'agent » (une session non reconnue ne doit montrer aucun marqueur, distinct
  d'« idle »). `pkg/session` et `pkg/gui` ne connaissent qu'une interface : un agent qui casse
  dégrade un marqueur, il ne casse rien d'autre.
- [x] Identification du process au premier plan du pty (`tcgetpgrp` → `/proc/<pid>/comm` sur Linux,
  `sysctl kern.proc.pid` sur macOS via `golang.org/x/sys/unix`) : dit **quel** agent tourne, jamais
  son état. Seul morceau non portable ajouté par la phase, isolé dans
  `pkg/session/foreground_{linux,darwin}.go` — premiers fichiers à build tag OS du dépôt.
- [x] **Manifestes de détection déclaratifs** (YAML, un par agent, embarqués dans le binaire via
  `//go:embed` pour `claude`/`codex`/`opencode`, surchargeables ou complétables dans
  `~/.config/lazyshell/agents/` — `config.AgentsDir()`, même racine XDG que `config.Path()`) évalués
  contre `pkg/screen.PlainTail` (nouveau : texte brut des lignes jusqu'au curseur, pas le SGR de
  `Render`) et le titre OSC. Pas de code Go par agent. **Jamais de mise à jour distante.**
- [x] `blocked` **strict**, vérifié à l'écriture du manifeste : `parseManifest` refuse une règle
  `blocked` qui suivrait une règle non-`blocked`, pour qu'un motif `working` trop large ne puisse
  jamais masquer un vrai prompt de permission. Défaut sans règle qui matche : `idle`.
- [x] Marqueur d'état dans la gouttière de `sessions_panel.go`, ajouté après `!`/`#`/`●`/`+` — en
  **couleur** (SGR brut vert/jaune/rouge/bleu, gocui étant déjà en `OutputTrue` depuis la phase 10).
  Détail non anticipé par ce plan : une couleur intégrée au texte de la gouttière rend la largeur en
  octets différente de la largeur visible, ce que le `%-4s` existant ne pouvait pas gérer — remplacé
  par un padding manuel sur la longueur visible (`gutterColumns`, passé à 5).
- [x] **Budget de rendu** : l'évaluation tourne dans la goroutine de drain (via un petit `io.Writer`
  intercalé devant `s.screen`, pour ne pas perturber le fast-path `io.Copy`/`WriteTo` dont dépendait
  un test existant), throttlée (≤ 1 fois / 500 ms / session), **jamais** dans la boucle de rendu.
  Affiné en cours de route : une vérification simplement « sautée » pendant la fenêtre de throttle
  pouvait perdre pour de bon l'état d'une salve de sortie qui se termine par un silence (exactement
  le cas d'un prompt de permission juste avant que l'agent n'attende) — un rattrapage différé unique
  est désormais armé pour la fin de la fenêtre. `TestIdleSessionDoesNotRepaint` reste à 0 repeint au
  repos.

*Critère de sortie de 11a — atteint : un manifeste de test (le mécanisme est vérifié de bout en
bout avec de vrais pty dans `pkg/session` et `pkg/gui`) fait passer l'état d'une session de `idle` à
`working` sans qu'aucun fichier de configuration n'existe.*

### 11b — Canal autoritatif (hooks des agents) — **fait, hors opencode**

Le seul canal qui donne l'état exact au lieu de le deviner. lazyshell n'appelle jamais l'agent : il
écoute.

- [x] Socket Unix par session (`0600`, dans `$XDG_RUNTIME_DIR/lazyshell/<pid>/`, repli sur
  `os.TempDir()` — `pkg/hook`), exposé aux sessions via `$LAZYSHELL_SOCK` + `$LAZYSHELL_SESSION_ID`
  (forcés dans l'environnement du process, avant les surcharges `env:` d'un projet, pour qu'un
  `lazyshell.yml` cloné ne puisse pas les réécrire). **Protocole entrant et déclaratif uniquement** :
  une ligne = un état (`idle`/`working`/`blocked`/`done`), pas de JSON, pas de verbe — un agent y
  déclare *son* état, il ne pilote rien (contrairement à l'API de contrôle de `herdr` — voir « ce qui
  reste ouvert »).
- [x] `lazyshell hook <event>` : la commande que l'utilisateur branche dans la config de son agent.
  Ne retourne jamais d'erreur — `$LAZYSHELL_SOCK` absent, état inconnu, socket disparu dégradent tous
  en diagnostic sur stderr, jamais en code de sortie non nul : un hook qui casserait la commande de
  l'agent serait pire que l'absence de marqueur.
- [x] Adaptateurs : **Claude Code** (hooks `settings.json` — `UserPromptSubmit`→`working`,
  `Notification`→`blocked`, `Stop`→`done`) ; **Codex** (clé racine `notify` de `~/.codex/config.toml`,
  événement `agent-turn-complete` → `done` seulement). **opencode reporté** : son signal le plus riche
  est un abonnement SSE `/event` (`session.status`, `permission.updated`) — tiré, pas poussé, une
  brique différente (client HTTP/SSE, découverte de l'URL du serveur) qui mérite son propre
  incrément plutôt que d'alourdir celui-ci ; décision prise avec l'utilisateur pendant le
  développement. Une session `opencode` reste sur le repli manifeste de 11a en attendant.
- [x] Le canal hook est **autoritaire** quand il rapporte ; les manifestes de 11a restent le repli.
  Tranché en implémentant : une fois un événement hook reçu, `evaluateAgentState` (11a) s'arrête
  *complètement* pour le reste de la vie de la session (`Session.hookDriven`), pas seulement le
  temps d'un arbitrage rejoué à chaque cycle — gain de lecture du code et de budget de rendu au
  passage. Une garde supplémentaire (`setAgentStateUnlessHookDriven`) évite qu'une évaluation manifeste
  déjà en vol au moment où le hook arrive n'écrase la valeur qu'il vient de poser.
- [x] `lazyshell init --agents` : affiche (sur stdout, jamais un fichier — `.claude/settings.json` et
  `~/.codex/config.toml` existent déjà et doivent être fusionnés à la main, pas écrasés) le bloc de
  configuration à coller chez l'agent, plutôt que de le faire recopier depuis le README.

### 11c — Notifications et ergonomie

- [ ] **Notification** sur `blocked` et sur `done`, via **OSC 9 / OSC 777 vers le terminal hôte** —
  pas via `notify-send`. Même raison qu'OSC 52 en phase 9 : ça traverse SSH et ça ne dépend d'aucun
  binaire installé. Commande externe configurable en repli.
- [ ] **Saut vers la prochaine session bloquée** en une touche. C'est ce qui justifie tout le reste :
  à 6 agents ouverts, on ne navigue plus, on répond à celui qui appelle.
- [ ] **Stats de tour** : durée du tour en cours, et tokens/coût **best-effort**. Les transcripts
  (`~/.claude/projects/**.jsonl`, qui portent un `message.usage` complet) et les bases
  (`~/.codex/*.sqlite`) ne sont **pas des contrats** — schémas internes, versionnés, migrés sans
  préavis. `ccusage` fait déjà ce travail : l'intégration est une **commande externe configurable
  dont on affiche la ligne** (modèle `statusLine` de Claude Code), pas une réimplémentation de la
  comptabilité des tokens.

**Critère de sortie** : un `lazyshell.yml` déclarant 4 sessions d'agents ; quand l'un demande une
permission, son marqueur passe `blocked` en moins d'une seconde et une notification part ; une
touche saute dessus ; les autres sessions continuent ; le nombre de repeints au repos est inchangé.

**Risque** : moyen, et entièrement concentré sur le **couplage à des formats non contractuels**.
Traitement : tout derrière `pkg/agent`, dégradation propre, et aucune fonctionnalité de lazyshell
qui dépende de la présence d'un agent.

---

## Séquencement et jalons

| Phase | Livrable | Jalon | État |
|---|---|---|---|
| 0 | squelette + CI | — | fait |
| 1 | spike pty + ADR rendu/clavier | **go / no-go technique** | fait |
| 2 | `pkg/session` testé | — | fait |
| 3 | UI 2 panneaux, lecture seule | démo interne | fait |
| 4 | pass-through interactif | **v0.1 — MVP dogfoodable** | fait |
| 5 | config, thème, aide, README | **v0.2 — publiable** | fait |
| 6 | `lazyshell.yml` de projet, sessions déclaratives | **v0.3** | fait |
| 7 | activité, relance, saut par index, zoom, aides contextuelles | **v0.4** | à faire |
| 8 | goreleaser, `--version`, bench de redraw en CI | **v0.5 — installable par un tiers** | fait |
| 9 | recherche, copy-mode, export, broadcast | v0.6 | fait |
| 10 | émulation terminal complète | **v1.0** | fait (en avance) |
| 11 | états d'agents IA, notifications, saut vers la session bloquée | **v1.1** | 11a/11b faits, 11c à faire |

## Décisions déjà actées (ne pas re-débattre)

- gocui `jesseduffield` + `boxlayout` + `creack/pty`.
- Keybindings plats (lazydocker), **pas** le pattern controller de lazygit.
- `TaskManager` pour les goroutines d'*affichage* uniquement ; le cycle de vie des process shell est
  détenu par `session.Manager`, découplé.
- Structure de packages : celle du rapport (`pkg/{app,session,gui,tasks,config}`).

## Ce qui reste ouvert

1. [x] Stratégie de rendu ANSI (tranchée en phase 1, revisitée et close en phase 10 — ADR 0002).
2. [x] Préfixe d'échappement du mode pass-through (`Ctrl-B`, remappable — ADR 0001).
3. [ ] Souris (sélection dans la liste, clic pour focus) : hors périmètre tant que gocui confond les
   boutons de souris avec les Shift-flèches (ADR 0001, décision reconduite en phase 10).
4. [ ] Portée du support Windows : hors périmètre (pas de pty Unix) — à assumer explicitement.
5. [x] **Tranché (phase 6)** : **strictement le cwd**, plus `--config-file` pour désigner un fichier
   ailleurs. Pas de remontée d'arborescence — le fichier qui s'exécute est toujours celui qu'on
   voit. Et un fichier de projet ne surcharge que **`shell` et `sessions`** : le thème et les
   keybindings restent la propriété de l'utilisateur. La remontée jusqu'à la racine du dépôt reste
   ouverte si le besoin se manifeste à l'usage ; elle rendrait les `cwd` relatifs plus ambigus et
   étendrait le modèle de confiance à des chemins qu'on n'a pas ouverts.
6. [x] **Tranché (phase 6)** : **approbation par chemin mémorisée**, invalidée par le hash du
   contenu (modèle `direnv`). Une confirmation à chaque lancement serait une friction quotidienne
   sur son propre projet, et on finirait par taper « y » sans lire — c'est-à-dire exactement
   l'inverse de ce que le garde-fou cherche à obtenir.
7. [ ] API de contrôle par les agents (phase 11) : `herdr` expose un socket qui laisse un agent créer
   des panneaux, lire la sortie des autres et réagir. Séduisant pour un « agent chef », mais c'est
   une surface d'exécution offerte à un processus non fiable. Le socket de la phase 11b est décidé
   **entrant et déclaratif** ; ouvrir un verbe de contrôle est une décision séparée, à ne pas
   prendre par glissement.
8. [ ] Détach / daemon (acté hors périmètre en phase 2) : c'est ce qui manque pour « mes agents
   tournent pendant que le laptop est fermé », le cas d'usage que `herdr` vend avec son serveur. À
   rouvrir seulement si la phase 11 le fait remonter comme demande réelle.
