# Analyse technique de lazygit / lazydocker — pour un gestionnaire de sessions shell TUI

## Résumé exécutif

lazygit et lazydocker sont tous deux construits sur **gocui** (fork de `jesseduffield/gocui`, lui-même
un fork de `awesome-gocui`/`termbox`), une lib TUI basée sur un modèle "vues nommées + boucle
d'événements + fonction de layout appelée à chaque frame". Le mécanisme le plus directement
réutilisable pour un gestionnaire de sessions shell est celui de **lazydocker** qui streame les logs
d'un container Docker en continu dans un panel : un `io.Writer` (la `gocui.View` elle-même) reçoit
en flux continu la sortie d'un `io.ReadCloser`, dans une goroutine annulable via `context.Context`,
pendant qu'une boucle de rafraîchissement périodique (`goEvery`) déclenche le redraw de l'écran. C'est
exactement le pattern à copier pour afficher la sortie live d'un process bash/zsh.

## Stack technique

- **gocui** : lib de TUI en mode "immediate-ish" — chaque `View` est un buffer de lignes avec
  position/dimensions, un curseur, un flag `Wrap`, un flag `Autoscroll`. `View` implémente
  `io.Writer`, donc tout ce qui sait écrire dans un `io.Writer` (comme `io.Copy`) peut écrire dans
  un panel.
- lazygit vendorise sa propre copie de gocui dans `pkg/gocui/` (fork poussé, avec tcell driver en
  option — voir `tcell_driver.go`) ; lazydocker dépend du module externe
  `github.com/jesseduffield/gocui v0.3.1-...`.
- lazydocker utilise en plus `github.com/jesseduffield/lazycore/pkg/boxlayout` pour le calcul de
  layout (partagé avec lazygit).
- Modules Go annexes notables : `github.com/docker/docker/pkg/stdcopy` (démultiplexage
  stdout/stderr du protocole Docker), `github.com/sasha-s/go-deadlock` (mutex avec détection de
  deadlock), `gopkg.in/yaml.v2` (config utilisateur).

### Structure des dossiers (lazydocker, la plus proche du besoin)

```
pkg/
  app/            bootstrap de l'application (charge config, crée le Gui, l'exécute)
  commands/       accès au moteur externe (ici Docker SDK) — équivalent futur: gestion des process shell
  config/         chargement config YAML utilisateur + defaults
  gui/            tout le TUI : gui.go (boucle), layout.go, arrangement.go (dimensions),
                   keybindings.go, container_logs.go, containers_panel.go, panels/ (abstraction
                   liste générique), presentation/ (formatage texte/couleurs)
  tasks/          gestionnaire de tâches asynchrones annulables (le cœur du streaming)
  i18n/           traductions
```

lazygit a la même colonne vertébrale mais en plus complexe : `pkg/gui/context/` (pile de contexts),
`pkg/gui/controllers/` (un fichier par domaine fonctionnel avec `GetKeybindings()`),
`pkg/gui/types/` (interfaces communes). Pour un projet from-scratch, la structure de lazydocker est
un bien meilleur point de départ (moins de sur-ingénierie).

## Architecture des panels / layout

Le layout n'est pas posé une fois pour toutes : gocui appelle une fonction "manager" à **chaque
frame / resize**, qui recalcule position et taille de toutes les vues.

`lazydocker/pkg/gui/gui.go` (init) :
```go
g.SetManager(gocui.ManagerFunc(gui.layout), gocui.ManagerFunc(gui.getFocusLayout()))
```

`lazydocker/pkg/gui/layout.go` :
```go
// layout is called for every screen re-render e.g. when the screen is resized
func (gui *Gui) layout(g *gocui.Gui) error {
	g.Highlight = true
	width, height := g.Size()

	appStatus := gui.statusManager.getStatusString()
	viewDimensions := gui.getWindowDimensions(gui.getInformationContent(), appStatus)

	setViewFromDimensions := func(viewName string, windowName string) (*gocui.View, error) {
		dimensionsObj, ok := viewDimensions[windowName]
		view, err := g.View(viewName)
		...
		_, err = g.SetView(
			viewName,
			dimensionsObj.X0-frameOffset, dimensionsObj.Y0-frameOffset,
			dimensionsObj.X1+frameOffset, dimensionsObj.Y1+frameOffset,
			0,
		)
		view.Visible = true
		return view, err
	}

	for _, viewName := range gui.autoPositionedViewNames() {
		setViewFromDimensions(viewName, viewName)
	}
	return gui.resizeCurrentPopupPanel(g)
}
```

Le calcul des dimensions lui-même est délégué à `boxlayout` (arbre de `Box` avec `Direction`
ROW/COLUMN, `Weight`, `Size` fixe optionnelle) — `lazydocker/pkg/gui/arrangement.go` :
```go
func (gui *Gui) getWindowDimensions(informationStr string, appStatus string) map[string]boxlayout.Dimensions {
	width, height := gui.g.Size()
	sideSectionWeight, mainSectionWeight := gui.getMidSectionWeights()
	sidePanelsDirection := boxlayout.COLUMN
	portraitMode := width <= 84 && height > 45
	if portraitMode {
		sidePanelsDirection = boxlayout.ROW
	}

	root := &boxlayout.Box{
		Direction: boxlayout.ROW,
		Children: []*boxlayout.Box{
			{
				Direction: sidePanelsDirection,
				Weight:    1,
				Children: []*boxlayout.Box{
					{Direction: boxlayout.ROW, Weight: sideSectionWeight, ConditionalChildren: gui.sidePanelChildren},
					{Window: "main", Weight: mainSectionWeight},
				},
			},
			{Direction: boxlayout.COLUMN, Size: infoSectionSize, Children: gui.infoSectionChildren(...)},
		},
	}
	return boxlayout.ArrangeWindows(root, 0, 0, width, height)
}
```
C'est exactement le modèle "flexbox simplifié" qu'on veut pour : panel gauche = liste de sessions
(poids fixe ou % de largeur), panel droit = sortie du process actif (poids restant), et bascule
automatique en mode "portrait" (empilement vertical) quand le terminal est étroit.

## Gestion du focus et de la navigation

`lazydocker/pkg/gui/focus.go` définit un second "manager" gocui appelé après le layout, qui détecte
les changements de vue courante et déclenche des hooks `onFocusLost` / `onFocus` :

```go
func (gui *Gui) getFocusLayout() func(g *gocui.Gui) error {
	var previousView *gocui.View
	return func(g *gocui.Gui) error {
		newView := gui.g.CurrentView()
		gui.onFocusChange()
		if newView != previousView && !gui.isPopupPanel(newView.Name()) {
			gui.onFocusLost(previousView, newView)
			gui.onFocus(newView)
			previousView = newView
		}
		return nil
	}
}

func (gui *Gui) onFocusChange() error {
	currentView := gui.g.CurrentView()
	for _, view := range gui.g.Views() {
		view.Highlight = view == currentView && view.Name() != "main"
	}
	return nil
}
```
La vue "active" est simplement mise en surbrillance (`view.Highlight`) ; le changement de focus se
fait via `gui.switchFocus(view)` (appelle `g.SetCurrentView` sous le capot) déclenché par des
handlers de touches (ex. Tab, clic souris, ou "entrer dans le panel main").

lazygit va plus loin avec une **pile de contexts** (`pkg/gui/context.go`, type `ContextMgr`) :
```go
type ContextMgr struct {
	ContextStack []types.Context
	...
}
```
`PushContext`/`PopContext` empilent/dépilent des contextes logiques (pas juste des vues — un
"context" peut être "liste des fichiers en mode normal" vs "liste des fichiers en mode diff"), ce
qui permet de restaurer l'état précédent (ex: "retour" avec Echap). Pour un gestionnaire de sessions
shell simple, une pile de contexts est probablement du sur-engineering au départ ; le modèle
`switchFocus` + historique simple de lazydocker suffit largement.

## Système de keybindings / controllers

lazydocker reste simple : une liste de `Binding` (vue cible, touche, handler) enregistrée une seule
fois au démarrage via `g.SetKeybinding` — `lazydocker/pkg/gui/keybindings.go` :

```go
type Binding struct {
	ViewName    string
	Handler     func(*gocui.Gui, *gocui.View) error
	Key         interface{}
	Modifier    gocui.Modifier
	Description string
}

func (gui *Gui) keybindings(g *gocui.Gui) error {
	bindings := gui.GetInitialKeybindings()
	for _, binding := range bindings {
		if err := g.SetKeybinding(binding.ViewName, binding.Key, binding.Modifier, binding.Handler); err != nil {
			return err
		}
	}
	if err := g.SetTabClickBinding("main", gui.onMainTabClick); err != nil {
		return err
	}
	return nil
}
```
`ViewName == ""` = binding global (fonctionne quelle que soit la vue active), sinon le binding n'est
actif que quand la vue nommée a le focus — géré en interne par gocui.

lazygit remplace cette liste plate par un pattern **"controller"** : chaque domaine (sync, branches,
fichiers...) est une struct qui implémente `GetKeybindings(opts) []*types.Binding` et expose des
métadonnées riches (description, tooltip, raison de désactivation dynamique) —
`lazygit/pkg/gui/controllers/sync_controller.go` :
```go
func (self *SyncController) GetKeybindings(opts types.KeybindingsOpts) []*types.Binding {
	return []*types.Binding{
		{
			Keys:              opts.GetKeys(opts.Config.Universal.Push),
			Handler:           opts.Guards.NoPopupPanel(self.HandlePush),
			GetDisabledReason: self.getDisabledReasonForPushOrPull,
			Description:       self.c.Tr.Push,
			Tooltip:           self.c.Tr.PushTooltip,
		},
		...
	}
}
```
Utile si l'app grossit (cheatsheet auto-générée, aide contextuelle), mais overkill pour un MVP.
**Recommandation : démarrer avec le modèle plat de lazydocker**, migrer vers des "controllers" si le
nombre d'actions explose.

## Streaming de sortie de commande en live vers une vue — LE POINT CRITIQUE

C'est le mécanisme central à répliquer. lazydocker l'utilise pour streamer `docker logs -f` dans le
panel principal quand on sélectionne un container et l'onglet "Logs".

### 1. Le gestionnaire de tâches annulables (`pkg/tasks/tasks.go`)

Chaque tâche tourne dans sa propre goroutine, avec un `context.Context` annulable ; lancer une
nouvelle tâche **stoppe automatiquement l'ancienne** (utile: changer de session doit tuer le stream
précédent) :

```go
func (t *TaskManager) NewTask(f func(ctx context.Context)) error {
	go func() {
		...
		ctx, cancel := context.WithCancel(context.Background())
		notifyStopped := make(chan struct{})

		if t.currentTask != nil {
			t.currentTask.Stop()   // annule + attend la fin de la tâche précédente
		}
		t.currentTask = &Task{ctx: ctx, cancel: cancel, notifyStopped: notifyStopped, f: f}

		go func() {
			f(ctx)
			close(notifyStopped)
		}()
	}()
	return nil
}

func (t *Task) Stop() {
	...
	t.cancel()
	<-t.notifyStopped   // attend que la goroutine ait bien fini avant de continuer
	t.stopped = true
}
```
Il existe aussi `NewTickerTask` (dans `pkg/tasks/tasks.go` et redéfini côté `pkg/gui/tasks_adapter.go`)
pour les vues qui doivent se re-rendre périodiquement (ex : stats CPU) plutôt qu'en flux continu.

### 2. Écriture directe dans la `View` (io.Writer) via `io.Copy` / `stdcopy.StdCopy`

`lazydocker/pkg/gui/container_logs.go` :
```go
func (gui *Gui) renderContainerLogsToMain(container *commands.Container) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			gui.renderContainerLogsToMainAux(container, ctx, notifyStopped)
		},
		Duration:   time.Millisecond * 200,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Autoscroll: true,
	})
}

func (gui *Gui) writeContainerLogs(ctr *commands.Container, ctx context.Context, writer io.Writer) error {
	readCloser, err := gui.DockerCommand.Client.ContainerLogs(ctx, ctr.ID, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true, ...
	})
	if err != nil {
		return err
	}
	defer readCloser.Close()

	if ctr.Details.Config.Tty {
		_, err = io.Copy(writer, readCloser)          // flux TTY : pas de démux nécessaire
	} else {
		_, err = stdcopy.StdCopy(writer, writer, readCloser)  // démultiplexe stdout/stderr Docker
	}
	return err
}
```
`writer` ici est directement `gui.Views.Main` (une `*gocui.View`). `View.Write([]byte)`
(`lazygit/pkg/gocui/view.go:847`) est **thread-safe** (protégé par un `sync.Mutex` interne,
`writeMutex`) donc on peut écrire dedans depuis n'importe quelle goroutine sans passer par
`g.Update` — gocui gère la concurrence à l'écriture pour vous :
```go
func (v *View) Write(p []byte) (n int, err error) {
	v.writeMutex.Lock()
	defer v.writeMutex.Unlock()
	v.write(p)   // marque v.tainted = true
	return len(p), nil
}
```
`v.tainted = true` marque juste la vue comme "à redessiner" ; **c'est un tick périodique séparé qui
déclenche le vrai redraw écran** (voir section suivante) — l'écriture elle-même ne pousse rien à
l'écran instantanément.

### 3. Transposition directe pour un shell bash/zsh

Le pattern est quasi identique à ce qu'il faut faire pour streamer un process shell :
```go
cmd := exec.CommandContext(ctx, "bash")
stdout, _ := cmd.StdoutPipe()
cmd.Stderr = cmd.Stdout // ou pty combiné
cmd.Start()
io.Copy(mainView, stdout)   // au lieu de readCloser Docker, un os/exec Pipe (ou un fd de pty)
```
Différence majeure à anticiper : les logs Docker sont un flux **sortant uniquement** (pas
d'interaction clavier→process). Pour un vrai shell interactif il faut en plus rediriger le **stdin**
de l'utilisateur vers le process (voir section pty ci-dessous) — chose que ni lazydocker ni lazygit
n'ont besoin de faire pour leurs logs, mais que lazydocker fait ponctuellement pour des commandes
interactives via `g.Suspend()`/`g.Resume()` (section suivante).

## Système de tâches asynchrones / synchronisation avec l'UI (pattern `g.Update`)

Le rafraîchissement écran n'est **pas** déclenché à chaque octet écrit. lazydocker utilise un tick
périodique global qui vérifie si une vue est "tainted" (modifiée depuis le dernier redraw) et
déclenche alors un `g.Update` (qui planifie un redraw sur la goroutine principale de gocui) —
`lazydocker/pkg/gui/gui.go` :
```go
go func() {
	throttledRefresh.Trigger()
	gui.goEvery(time.Millisecond*30, gui.reRenderMain)
	gui.goEvery(time.Millisecond*1000, gui.updateContainerDetails)
	gui.goEvery(time.Millisecond*1000, gui.checkForContextChange)
	gui.goEvery(time.Millisecond*1000, gui.renderContainersAndServices)
}()

err = g.MainLoop()
```
```go
func (gui *Gui) reRenderMain() error {
	mainView := gui.Views.Main
	if mainView.IsTainted() {
		gui.g.Update(func(g *gocui.Gui) error { return nil })
	}
	return nil
}
```
`goEvery` (même fichier) est un simple wrapper `time.Ticker` + goroutine :
```go
func (gui *Gui) goEvery(interval time.Duration, function func() error) {
	_ = function()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if !gui.PauseBackgroundThreads {
				_ = function()
			}
		}
	}()
}
```
**Règle d'or à retenir** : on peut écrire dans une `View` depuis n'importe quelle goroutine (c'est
mutex-protégé), mais toute autre mutation d'état gocui (changer la vue courante, changer les
dimensions, etc.) doit passer par `g.Update(func(g *gocui.Gui) error {...})`, qui met la closure
dans une queue exécutée sur la goroutine principale du `MainLoop`. C'est le pattern classique
"single-threaded UI thread + message queue" — évite toute race sur l'état interne de gocui.

## Gestion des process externes (os/exec)

Deux cas distincts dans lazydocker :

1. **Lecture de flux en arrière-plan sans reprendre le terminal** (le cas des logs, transposable au
   shell) : le process/flux tourne pendant que gocui garde le contrôle du terminal, la sortie est
   copiée dans une `View`.

2. **Commande interactive qui a besoin du terminal réel** (ex: `docker exec -it`, ou une commande
   custom) — `lazydocker/pkg/gui/subprocess.go` :
```go
func (gui *Gui) runSubprocessWithMessage(cmd *exec.Cmd, msg string) error {
	gui.g.Suspend()              // rend le terminal à l'OS, gocui arrête de dessiner
	gui.PauseBackgroundThreads = true
	gui.runCommand(cmd, msg)     // cmd.Stdout/Stderr/Stdin = os.Stdout/os.Stdin directement
	gui.g.Resume()               // gocui reprend le contrôle du terminal
	gui.PauseBackgroundThreads = false
	return nil
}

func (gui *Gui) runCommand(cmd *exec.Cmd, msg string) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	cmd.Stdin = os.Stdin
	...
	cmd.Run()
	...
}
```
Ni lazygit ni lazydocker n'utilisent de **pty** (pseudo-terminal) — ils n'en ont pas besoin car ils
streament soit du texte pur (logs), soit délèguent carrément le terminal via `Suspend`/`Resume`.
**Pour un vrai gestionnaire de sessions shell multiplexées (l'équivalent tmux/screen), c'est
insuffisant** : un shell interactif a besoin d'un pty pour gérer correctement les caractères de
contrôle, la taille de fenêtre (`SIGWINCH`), les couleurs, les prompts interactifs, le job control,
etc. — voir recommandations ci-dessous.

## Système de listes synchronisées avec la sélection

lazydocker généralise le pattern "liste à gauche qui pilote le contenu du panel main" via
`SideListPanel[T]` (générique Go) — `lazydocker/pkg/gui/panels/side_list_panel.go` :
```go
type SideListPanel[T comparable] struct {
	ContextState *ContextState[T]
	ListPanel[T]
	OnSelect func(T) error
	GetTableCells func(T) []string
	...
}
```
Chaque panel de liste (containers, services, images...) instancie ce générique avec son propre type
métier, fournit `GetTableCells` (colonnes affichées) et `OnSelect` (déclenché à chaque changement de
ligne sélectionnée, typiquement pour lancer une nouvelle tâche de rendu dans le panel main — via
`QueueTask`). C'est exactement l'abstraction à répliquer pour une liste de "sessions shell" (nom,
statut running/exited, PID, cwd) à gauche.

## Configuration et thème

Config YAML utilisateur fusionnée avec des defaults en dur (Go structs + tags `yaml:"...,omitempty"`)
— `lazydocker/pkg/config/app_config.go` :
```go
func loadUserConfig(configDir string, base *UserConfig) (*UserConfig, error) {
	...
	if err := yaml.Unmarshal(content, base); err != nil { ... }
}
```
Couleurs définies en YAML (`activeBorderColor`, `inactiveBorderColor`, `selectedLineBgColor`...),
converties en `gocui.Attribute` puis appliquées globalement au `*gocui.Gui`
(`lazydocker/pkg/gui/theme.go`) :
```go
func (gui *Gui) SetColorScheme() error {
	gui.g.FgColor = GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.InactiveBorderColor)
	gui.g.SelFgColor = GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.ActiveBorderColor)
	gui.g.FrameColor = gui.g.FgColor
	gui.g.SelFrameColor = gui.g.SelFrameColor
	return nil
}
```
lazygit a un package `style/` plus riche (styles composables : gras, couleur, fond, decoration) mais
le principe reste : mapper la config YAML vers des `gocui.Attribute`/couleurs 256/truecolor au
démarrage.

## Recommandations concrètes pour le projet "gestionnaire de sessions shell"

### Réutilisable quasiment tel quel
- **gocui** (`github.com/jesseduffield/gocui`, ou vendoriser comme lazygit si besoin de patches) :
  boucle principale, `View` en `io.Writer` thread-safe, `Suspend`/`Resume`.
- **`lazycore/pkg/boxlayout`** : moteur de layout en arbre pondéré (row/column, weight, size fixe) —
  parfait pour "liste sessions à gauche (largeur fixe ou 20%) / output à droite (reste)", avec bascule
  portrait automatique.
- Le pattern **`TaskManager`** de lazydocker (`pkg/tasks/tasks.go`) tel quel : une tâche = une
  goroutine + `context.CancelFunc`, `NewTask` stoppe automatiquement la tâche précédente. Directement
  adaptable pour "changer de session sélectionnée = stopper le stream de lecture de la session
  précédente et démarrer celui de la nouvelle" (mais attention : il ne faut PAS tuer le process shell
  lui-même en changeant de sélection, seulement le goroutine qui *lit* sa sortie vers la vue — un vrai
  gestionnaire de sessions doit garder les process vivants en arrière-plan même non affichés,
  contrairement aux logs Docker qui peuvent se re-streamer à volonté).
- Le pattern d'écriture `io.Copy(view, pipe)` / `stdcopy` pour le rendu de sortie.
- Le modèle plat de `Binding`/`SetKeybinding` de lazydocker pour les raccourcis.
- `SideListPanel[T]` générique pour la liste de sessions.

### À ajouter, absent de lazygit/lazydocker
- **`github.com/creack/pty`** : indispensable. Contrairement aux logs Docker (flux sortant only) ou
  au `Suspend`/`Resume` (délègue tout le terminal), un multiplexeur de sessions shell doit :
  - allouer un pty par session (`pty.Start(cmd)` retourne un `*os.File` qui sert à la fois de stdin
    et stdout du shell) ;
  - propager le resize du panel au pty (`pty.Setsize` sur `SIGWINCH` du panel, recalculé dans la
    fonction `layout()` à chaque frame — c'est là que boxlayout donne la taille exacte du panel
    output) ;
  - rediriger les touches tapées par l'utilisateur (capturées par gocui quand le panel "output" a le
    focus) vers `pty.Write()` au lieu de les laisser gocui les interpréter comme des raccourcis —
    cela nécessite un **mode "insert"/"pass-through"** similaire à ce que fait `text_area.go` de
    gocui pour les champs de saisie, mais où chaque octet part vers le process au lieu d'un buffer
    local ;
  - garder un buffer circulaire (scrollback) car le pty ne rejoue pas l'historique comme
    `docker logs --since` — il faut soi-même stocker les octets déjà écrits dans la `View` (ou dans
    un buffer séparé) pour permettre le scroll.
- **Persistance de process en arrière-plan** : contrairement à Docker (le daemon garde les
  containers vivants indépendamment de lazydocker), ici *votre programme* doit être le process
  parent qui garde les shells vivants entre deux sélections dans la liste — il faut donc un
  `map[sessionID]*Session{cmd *exec.Cmd, pty *os.File, scrollback *bytes.Buffer, cancel func()}`
  vivant dans l'état du `Gui`, indépendant du `TaskManager` (qui ne doit gérer QUE le goroutine de
  *lecture/affichage*, pas le cycle de vie du process).

### Proposition d'architecture de départ

```
pkg/
  app/            bootstrap : charge config, construit sessionManager, lance gui.Run()
  session/        SessionManager : CRUD des sessions (New, Kill, List), chaque Session
                   possède cmd *exec.Cmd, ptmx *os.File, scrollback *ring.Buffer ou bytes.Buffer,
                   status (Running/Exited), createdAt
  gui/
    gui.go         init gocui, MainLoop, goEvery(reRenderOutput), keybindings
    layout.go      boxlayout: panel "sessions" (gauche, largeur fixe ~30) + panel "output" (droite)
    sessions_panel.go   SideListPanel[*session.Session]-like : liste + OnSelect -> QueueTask(stream)
    output.go       QueueTask qui fait io.Copy(outputView, session.PtyReader) via TaskManager
    input.go         capture des touches quand le panel output a le focus -> ptmx.Write(bytes)
    keybindings.go   n: nouvelle session, x/d: kill session, tab: cycle focus, ctrl+ tab liste<->output
    theme.go / config/  reprise directe du modèle YAML de lazydocker
  tasks/           copie quasi telle quelle de pkg/tasks de lazydocker (TaskManager, NewTickerTask)
```

Flux typique : sélection d'une session dans la liste → `OnSelect` appelle
`gui.QueueTask(func(ctx) { io.Copy(outputView, session.Reader()) })` (le `TaskManager` tue
automatiquement le stream de lecture précédent) ; taper au clavier quand le focus est sur le panel
output écrit directement dans `session.ptmx` ; un goroutine séparé (indépendant du TaskManager,
lancé à la création de la session et vivant tant que le process tourne) lit en continu le pty dans
le scrollback buffer de la session, et la tâche de streaming affichée ne fait que rejouer/suivre ce
buffer — ce qui permet de ne perdre aucune sortie produite pendant qu'une session n'est pas affichée
à l'écran.
