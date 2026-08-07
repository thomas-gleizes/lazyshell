# ADR 0002 — Rendu multi-panneaux, curseur et applications plein écran

- **Statut** : accepté.
- **Date** : 2026-08-06
- **Contexte** : phase 10 de `ROADMAP.md` — l'ex-phase 6, renumérotée depuis. Clôt le point laissé
  ouvert par l'ADR 0001 (« descendre vers `tcell` pour le panneau output ? ») et le point ouvert
  n° 1 de la roadmap (« stratégie de rendu ANSI »).

> **Note de numérotation.** L'ADR 0001 parle de « la phase 6 » pour désigner l'émulation de
> terminal complète : c'était son numéro à l'époque. La roadmap a depuis inséré quatre phases
> (config de projet, ergonomie multi-sessions, distribution, recherche/copie) et l'émulation est
> devenue la phase 10. L'ADR 0001 n'est pas réécrit — un ADR consigne ce qui était vrai quand il a
> été pris.
>
> Cette phase a en pratique été faite en avance sur le séquencement, parce que la décision 1
> ci-dessous corrige un défaut de la v0.2 **déjà publiée** et non une fonctionnalité future.

## Contexte

L'ADR 0001 a avancé l'émulateur de terminal en phase 1 et conclu que `vim`, `htop` et `less`
« deviennent possibles ». Possibles, mais pas fonctionnels : seul `cmd/spike-pty` en profitait.
`pkg/gui`, construit ensuite, avait perdu en route trois réglages que le spike avait validés, et
n'utilisait aucune des informations que l'émulateur expose au-delà du texte rendu.

Concrètement, avant cette phase, dans l'application réelle :

- tout prompt thémé, tout colorscheme `vim`, toute barre `htop` s'affichait avec des `[38;5;2m`
  en clair au milieu de l'écran ;
- `Esc` — la touche centrale de `vim` — n'était pas livrée de façon fiable ;
- le curseur n'était jamais dessiné : on tapait à l'aveugle ;
- rien ne distinguait « une commande a produit de la sortie » de « `vim` a la main », alors que
  `pkg/screen.IsAltScreen()` existait depuis la phase 1 et n'était appelée nulle part.

## Décision 1 — `OutputTrue` et `InputEsc` dans l'application réelle

`pkg/gui/gui.go` initialisait gocui en `OutputNormal`. Le spike documentait déjà que `OutputTrue`
est « required, not cosmetic » ; la mesure de l'ADR 0001 explique pourquoi, et le mécanisme est
visible dans gocui : `escape.go`, fonction `csiColor`, renvoie `errCSIParseError` pour la forme
256 couleurs en dessous d'`Output256` et pour la forme truecolor en dessous d'`OutputTrue` — et
une séquence rejetée n'est pas ignorée, **son corps est imprimé**.

Or `pkg/screen` émet précisément ces formes : c'est ce que produit `uv.Lines.Render` dès qu'une
cellule porte autre chose qu'une des 8 couleurs ANSI de base, ce qui est le cas de n'importe quel
thème de shell.

`g.InputEsc = true` est repris du spike pour la même raison : sans lui, un `Esc` isolé est retenu
le temps de voir si une séquence d'échappement suit.

Le thème n'est pas affecté : `getTcellColor` (gocui `attribute.go`) renvoie la couleur telle
quelle en `OutputTrue`, donc les constantes `gocui.ColorX` et `GetColor` de `pkg/gui/theme.go`
gardent leur sens. C'est vérifié par un test plutôt que par ce raisonnement.

## Décision 2 — Rester sur `gocui.View` ; pas de descente vers `tcell`

L'ADR 0001 laissait ouvert : « `gocui.View` reçoit une chaîne rendue à chaque frame, ce qui
fonctionne mais n'est pas un rendu cellule par cellule. Si le coût s'avère trop élevé avec
plusieurs panneaux, la question se reposera. »

Elle ne se repose pas. Mesures (`go test -bench . ./pkg/screen ./pkg/gui`, Intel Core Ultra 7 255U,
émulateur alimenté de 20 000 lignes de sortie colorée), à comparer au tick de rendu de **30 ms** :

| Opération | Coût | Part du tick |
|---|---|---|
| `Screen.Render`, 80×24 | 62 µs | 0,2 % |
| `Screen.Render`, 200×50 | 219 µs | 0,7 % |
| `Screen.Render`, 300×80 | 521 µs | 1,7 % |
| `Screen.RenderAt` (défilé), 80×24 | 32 µs | 0,1 % |
| `buildOutputFrame` (rendu + curseur), 80×24 | 46 µs | 0,15 % |
| `sessionsPanelContent`, 16 sessions | 6,4 µs | 0,02 % |

Deux choses à retenir :

1. **Le coût ne dépend que de la géométrie**, jamais du volume de sortie — c'est l'acquis de
   l'ADR 0001, et il tient aussi sur un panneau de 300×80.
2. **Un seul panneau est rendu à la fois.** Le nombre de sessions n'entre dans le budget que par
   la liste, qui coûte des microsecondes. « Multi-panneaux » ne multiplie pas le coût de rendu.

`RenderAt` est *moins* cher que `Render` : les lignes sorties de l'écran sont conservées telles
quelles par l'émulateur, alors que l'écran vivant est reconstruit cellule par cellule.

Un rendu cellule par cellule sous `tcell` échangerait tout ça contre un gestionnaire de vues à
écrire à la main, en perdant les popups, les bordures et le thème de gocui. Le coût réel n'était
pas là — voir la décision 3.

## Décision 3 — Ne pas repeindre un panneau inchangé

Le vrai coût n'était pas le rendu, c'était sa fréquence. Les deux panneaux sont pilotés par un
ticker de 30 ms, et chacun appelait `g.Update` à **chaque** tick, que quoi que ce soit ait changé
ou non. gocui n'a pas de redraw partiel : un `g.Update` relaie une passe de layout et repeint
l'écran entier.

Mesuré avec un `gocui.Manager` compteur et une session `/bin/sh` au repos
(`TestIdleSessionDoesNotRepaint`) :

| | Repeints par seconde, au repos |
|---|---|
| Avant | **60** |
| Après | **0** |

Le correctif est de comparer ce qu'on s'apprête à pousser avec ce qui a été poussé la fois
précédente, et de ne rien faire si c'est identique. Deux points de conception valaient d'être
tranchés :

- **L'état de comparaison du panneau output vit dans la closure de la tâche**, pas sur `Gui` : le
  `TaskManager` exécute cette fonction en série dans une seule goroutine, donc aucun verrou n'est
  nécessaire, et une nouvelle tâche repart avec « rien de dessiné », ce qui fait que le premier
  tick après n'importe quel `showOutput` redessine toujours. Celui de la liste des sessions est
  sur `Gui` sous `mu`, parce qu'il est appelé depuis deux goroutines.
- **La frame comparée inclut le curseur**, pas seulement le texte. Le curseur bouge sans que
  l'écran change (frappe au clavier avant l'écho, déplacement dans un buffer `vim` statique) ;
  l'omettre le laisserait visiblement coincé.

Corollaire : tout ce qui change l'*apparence* sans changer la *sortie* doit redémarrer la tâche de
rendu — changement de sélection, de décalage de défilement, de mode, de focus. C'est le motif que
le `TaskManager` porte déjà depuis la phase 3, étendu à deux cas de plus.

## Décision 4 — Le curseur est dessiné par le panneau, sous conditions

`vt` expose la position du curseur mais **pas** sa visibilité (`setCursorHidden` n'est pas
exporté). `pkg/screen` la suit via `vt.Callbacks.CursorVisibility`, en même temps que le titre
OSC, la cloche et le mode DECCKM — un seul bloc de callbacks posé à la construction.

Ces callbacks se déclenchent depuis `term.Write`, donc **avec `s.mu` déjà tenu**. C'est ce qui
permet aux handlers d'écrire les champs directement, et c'est aussi le piège du fichier : un
handler qui rappellerait une méthode de `Screen` se bloquerait sur un mutex qu'il détient déjà.
C'est documenté sur la structure.

gocui dessine le curseur seulement si les quatre conditions sont réunies : le panneau output a le
focus, le pass-through est armé, le défilement est à zéro (défilé, on regarde un historique où le
curseur n'est pas), et l'application n'a pas caché le curseur. La perte de focus l'éteint
immédiatement via un hook `onFocusLost`, sans attendre la prochaine frame — sinon un curseur
resterait à clignoter sur un panneau que le clavier ne pilote plus.

## Décision 5 — L'alternate screen s'affiche, il ne change pas de mode

Quand une session passe en écran alterné, lazyshell le **signale** — `[ALT]` dans la barre de
statut pour la session sélectionnée, marqueur `#` dans la gouttière de la liste pour toutes — et
neutralise le défilement dans le scrollback, que l'écran alterné n'alimente pas et dont les
touches appartiennent de toute façon à l'application en contrôle.

Il ne bascule **pas** automatiquement en pass-through. L'alternative était tentante (lancer `vim`
donnerait le clavier tout seul), mais elle change silencieusement ce que fait `q` : quitter
l'application, ou partir au shell. L'ADR 0001 avait déjà posé que l'utilisateur doit toujours
savoir dans quel mode il est ; un changement de mode qu'il n'a pas demandé va contre.

## Décision 6 — Mode curseur applicatif (DECCKM)

`pkg/keys` encodait les flèches en dur dans leur forme CSI (`ESC[A`). `vim`, `less` et une bonne
partie du monde ncurses arment DECCKM (`ESC[?1h`) à l'entrée, et certains n'acceptent que la forme
SS3 (`ESC OA`) — la forme CSI y tape une lettre au lieu de déplacer.

`pkg/screen` suit le mode via `Callbacks.EnableMode`/`DisableMode`, et `keys.TranslateWithMode`
substitue les formes SS3 pour les flèches et `Home`/`End`. Les flèches **modifiées** (Shift-Haut)
restent en CSI même sous DECCKM, comme dans xterm : il n'existe pas de forme SS3 portant un
modificateur.

`keys.Translate` reste l'appel en mode normal, pour ne pas invalider la table de tests de la
phase 1.

## Ce qui reste hors périmètre

- **La souris** reste désactivée, pour la raison technique déjà documentée dans l'ADR 0001 : gocui
  réutilise les mêmes valeurs pour les boutons de souris et les Shift-flèches.
  *(Levé en phase 12 — `docs/adr/0003-souris.md`. La collision ne portait que sur deux valeurs ; le
  reste du raisonnement de cette section tient toujours.)*
- **Les protocoles clavier étendus** (Kitty, CSI u, `modifyOtherKeys`) : gocui livre des
  événements déjà décodés, on ne peut pas encoder ce qu'il ne distingue pas.
- **Le collage entre crochets** (bracketed paste) : les touches sont synthétisées une par une, il
  n'y a pas de notion de collage à transmettre.

## Vérification

Automatisée, sans terminal :

- `pkg/gui` : une séquence 256 couleurs et une séquence truecolor sont **consommées** par la vue
  et non imprimées — les deux tests échouent si l'on repasse le harnais en `OutputNormal`, ce qui
  a été vérifié en le faisant.
- `pkg/gui` : le thème garde des couleurs distinctes et résolues en `OutputTrue`.
- `pkg/gui` : le curseur n'est porté par la frame que sur l'écran vivant, en pass-through, et
  seulement si l'application ne l'a pas caché ; un déplacement de curseur seul produit bien une
  frame différente.
- `pkg/gui` : le défilement est neutralisé en écran alterné (avec un scrollback réel, pour que le
  non-effet ne s'explique pas par un historique vide) ; l'indicateur `[ALT]` apparaît ; la
  gouttière porte `!` et `#` ; la cloche est acquittée à la sélection ; le titre OSC remplace le
  cwd.
- `pkg/gui` : `TestIdleSessionDoesNotRepaint` fait tourner une vraie `MainLoop` et compte les
  passes de layout.
- `pkg/screen` : position et visibilité du curseur, DECCKM, titre OSC, cloche à verrou.
- `pkg/keys` : formes SS3 sous DECCKM, CSI sinon, flèches modifiées inchangées, reste de la table
  inchangé.

Manuelle, dans un vrai terminal (`go run ./cmd/lazyshell`) : `vim`, `htop`, `less`, le
redimensionnement pendant qu'une application plein écran tourne, et le retour au shell après en
être sorti.

## Conséquences

- La phase 10 de la roadmap est atteinte pour ce qu'il en restait après l'ADR 0001 : l'émulateur
  était en place, il fallait l'intégrer.
- La phase 8 (« budget de redraw mesuré, pas commenté ») est amorcée : les benchmarks de cette
  phase sont le socle du seuil à verrouiller en CI ; il reste à les y brancher.
- Le point ouvert n° 1 de la roadmap (stratégie de rendu ANSI) est clos : émulateur + `gocui.View`
  + `OutputTrue`, sans descente vers `tcell`.
- `pkg/screen` est devenu la source unique de l'état du terminal émulé — texte, curseur, modes,
  titre, cloche — et non plus seulement un producteur de chaînes à afficher. C'est ce que
  l'ADR 0001 appelait « la session expose *donne-moi l'état à afficher*, pas *voici un flux
  d'octets* ».
- Le risque assumé de l'ADR 0001 sur `charmbracelet/x/vt` (module non versionné) augmente
  mécaniquement : on dépend maintenant de ses callbacks, pas seulement de `Render`. La surface
  reste concentrée dans `pkg/screen`.
