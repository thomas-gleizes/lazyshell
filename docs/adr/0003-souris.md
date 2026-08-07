# ADR 0003 — Souris

- **Statut** : accepté.
- **Date** : 2026-08-07
- **Contexte** : phase 12 de `ROADMAP.md`. Clôt le point ouvert n° 3 (« Souris (sélection dans la
  liste, clic pour focus) : hors périmètre tant que gocui confond les boutons de souris avec les
  Shift-flèches »), reconduit en phase 10 par l'ADR 0002.

## Contexte

La souris était hors périmètre depuis l'ADR 0001, pour une raison technique et non par
priorisation : gocui n'a pas de constantes propres pour les boutons de souris et réutilise des
touches de fonction inutilisées de tcell, dont deux servent déjà aux Shift-flèches.

En relisant le source de gocui avant d'ouvrir la phase, la collision s'est avérée **plus étroite que
ce que l'ADR 0001 laissait entendre**. Dans `keybinding.go` :

```
KeyShiftArrowUp   = Key(tcell.KeyF62)      MouseRight = Key(tcell.KeyF62)
KeyShiftArrowDown = Key(tcell.KeyF63)      MouseLeft  = Key(tcell.KeyF63)
```

Deux valeurs, pas plus. `MouseMiddle`, `MouseRelease` et les quatre `MouseWheel*` occupent
`F56`–`F61` et ne collisionnent avec rien : lazyshell n'utilise ni Shift-Gauche ni Shift-Droite, et
`KeyArrowLeft`/`KeyArrowRight` sont des constantes tcell distinctes.

Le vrai risque était ailleurs, et il n'était pas documenté. gocui essaie d'abord les *bindings de
souris*, puis retombe sur `execKeybindings`, qui consulte l'`Editor` de la vue courante. Le panneau
de sortie est `Editable` — c'est ce qui permet au mode pass-through d'exister. Donc, avec
`g.Mouse = true` et sans garde, **un clic gauche entrait dans `editOutput`, y était vu comme
`KeyShiftArrowDown`, et `\x1b[1;2B` était tapé dans le shell**.

Enfin, la demande d'usage qui a rouvert le sujet n'était pas « pouvoir cliquer » mais la molette :
sur un CLI d'agent IA, faire défiler à la molette rappelait l'historique des commandes au lieu de
remonter dans la sortie. C'est le comportement du terminal *hôte*, qui traduit la molette en
flèches quand aucune application ne réclame la souris.

## Décisions

### 1. La souris est active par défaut, et coupable par configuration

`mouse.enabled`, défaut `true`. Le prix — ne plus transmettre `Maj-Haut`/`Maj-Bas` à la session —
est réel, mesuré, et concerne deux touches sans usage établi dans lazyshell. `mouse.enabled: false`
les rend intégralement : gocui n'émet alors aucun événement de souris, il n'y a rien à filtrer, et
la valeur ambiguë ne peut plus être qu'une vraie Shift-flèche.

### 2. L'arbitrage se fait en un seul point

`editOutput` écarte `gocui.IsMouseKey(key)` en tête, avant `keys.Normalize`. C'est la seule ligne de
code où la décision « cette valeur est une souris, pas une touche » est prise, et elle est prise
avant tout ce qui pourrait écrire dans un pty. `pkg/keys` garde ses deux entrées Shift-flèches
inchangées : elles restent correctes, elles deviennent seulement inatteignables quand la souris est
active.

Une seconde garde, `g.ShouldHandleMouseEvent`, refuse en amont les événements sur le panneau de
sortie pendant le pass-through — sans elle gocui déplacerait le curseur d'une vue dont l'émulateur
est propriétaire.

### 3. La molette fait défiler le contenu, jamais l'historique

C'est la décision structurante de la phase, et elle est plus forte qu'un réglage : **une molette
n'est jamais encodée en flèche**, dans aucun état. Activer `g.Mouse` supprime la cause du problème,
puisque lazyshell capte lui-même l'événement au lieu de laisser le terminal hôte le traduire.

Une molette ne quitte lazyshell que si le programme de la session a explicitement armé un mode de
suivi souris (décision 5) — ce qu'un shell et un CLI d'agent ne font jamais. Sur l'écran alterné
sans suivi armé, la molette ne fait **rien** : il n'y a pas de scrollback derrière une application
plein écran, donc ne rien faire est la bonne réponse, pas un oubli.

### 4. Un clic est une navigation, un double-clic un engagement

Cliquer une session la sélectionne sans armer le pass-through, exactement comme `j`/`k`. Le
double-clic est le geste délibéré, l'équivalent souris de `Entrée`. Sans cette distinction, on
prend la main sur un shell en cliquant simplement à côté.

La correspondance ligne → session est directe et garantie : `sessionsPanelContent` impose
« exactement une ligne par session », contrainte dont dépendent déjà `view.SetCursor` et
`Highlight`. Le clic n'introduit donc aucune arithmétique nouvelle.

Un glissé sélectionne des lignes en copy-mode, et **relâcher ne copie rien** : la sélection reste
posée, `y` la copie. Écrire dans le presse-papier de quelqu'un doit rester une action demandée, pas
l'effet de bord d'un mouvement de pointeur.

### 5. L'application invitée reçoit la souris seulement si elle la demande

`pkg/screen` mémorise désormais les DECSET 9/1000/1002/1003 et 1006, `pkg/keys.EncodeMouse` les
ré-encode en SGR ou dans la forme historique. Le critère de routage n'est jamais une supposition sur
le programme qui tourne : `vim` avec `set mouse=a` demande, `htop` demande, un shell et un CLI
d'agent ne demandent pas.

## Ce qui reste hors périmètre, et pourquoi

- **Les modificateurs.** gocui ne livre pas de façon fiable Shift/Ctrl avec un clic — sa propre
  documentation le déconseille explicitement. Un `Ctrl+clic` n'est donc pas transmis.
- **Le bouton du relâchement.** `MouseRelease` est générique : gocui ne dit pas quel bouton est
  remonté. La forme historique (X10) ne le dit pas non plus, mais SGR si — d'où la préférence pour
  SGR dès que l'application l'a demandé.
- **La sélection par colonnes.** `pkg/screen/selection.go` sélectionne des lignes entières. Le
  glissé hérite de cette limite ; l'étendre est une évolution de `pkg/screen`, pas de la souris.
- **Les encodages 1005 / 1015 / 1016.** Toute application qui les demande demande aussi SGR.
  Inventer un support partiel serait pire que de ne pas en avoir.
- **Le survol.** gocui livre les mouvements sans bouton sur un chemin distinct, sans point
  d'accroche public. Rien n'en dépend aujourd'hui.

## Vérification

- `pkg/gui/mouse_test.go` : `TestEditOutputDropsMouseKeys` est le test central — sans la garde de la
  décision 2, il constate `\x1b[1;2B` tapé dans le shell. Son miroir,
  `TestEditOutputKeepsShiftArrowsWhenMouseOff`, verrouille l'autre moitié du marché.
- Clic hors bornes, liste vide, clic simple qui n'arme pas le pass-through, double-clic qui l'arme,
  molette qui bouge l'offset, molette sans effet sur l'écran alterné, glissé qui pose une plage.
- `pkg/keys/mouse_test.go` : tables SGR et X10, plafond de 223 cellules, filtrage par mode.
- `pkg/screen/screen_test.go` : les DECSET arment et désarment le suivi ; désactiver un mode
  *inactif* ne désarme pas celui qui l'est.
