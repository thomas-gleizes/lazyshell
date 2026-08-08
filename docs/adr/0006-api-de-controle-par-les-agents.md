# ADR 0006 — API de contrôle par les agents

- **Statut** : accepté.
- **Date** : 2026-08-08
- **Contexte** : renverse une décision explicite prise en phase 11 (socket **entrant et
  déclaratif**, aucun verbe) et consignée à trois endroits : le doc de package de `pkg/hook`, le
  commentaire de `Session.SetAgentState`, et la section « Écarté, explicitement » du
  `RAPPORT_ANALYSE_INTEGRATION_AGENTS_IA`. Complète l'ADR 0005 sans le toucher : ce document ne
  parle ni de clavier ni de rendu.

## Contexte

La phase 11b a donné à chaque session un socket Unix, exposé au processus qui y tourne sous
`$LAZYSHELL_SOCK`, et un protocole d'une pauvreté volontaire : une ligne, parmi quatre mots
(`idle`, `working`, `blocked`, `done`). Le doc de `pkg/hook` en faisait un principe :

> No JSON, no framing, no verbs beyond "this is my state now" — an agent declares itself, it does
> not control lazyshell.

Le rapport de phase 11 rangeait l'API de contrôle à la `herdr` (un agent qui crée des panneaux et
lit la sortie des autres) dans « écarté, explicitement » :

> c'est une surface d'exécution offerte à un processus non fiable, pour un usage de niche. […] À
> reverser dans « ce qui reste ouvert », pas dans la phase.

La formulation retenue dans la roadmap était plus précise encore : *« ouvrir un verbe de contrôle
est une décision séparée, à ne pas prendre par glissement »*. C'est ce glissement qui était le
risque — pas la fonctionnalité. Un jour, un besoin réel (un agent chef qui délègue à des sessions
sœurs), une petite extension du protocole existant « puisqu'il est déjà là », et la propriété
« lazyshell n'obéit à personne » disparaît sans que personne ne l'ait décidée.

Ce document prend la décision, dans l'autre sens, et en énonce le prix.

## Décision 1 — L'API existe, dans son propre package et sur son propre socket

`pkg/control` expose six verbes — `list`, `read`, `new`, `send`, `kill`, `rename` — sur un socket
Unix distinct de celui de `pkg/hook`, avec un protocole distinct : un objet JSON par ligne, une
requête, une réponse. La CLI cliente est `lazyshell ctl`, symétrique de `lazyshell hook`.

Deux packages plutôt qu'un, et deux sockets plutôt qu'un, pour une raison qui n'est pas cosmétique :
le canal de `pkg/hook` reste **ouvert par défaut**, et il ne peut le rester que parce que son
pouvoir maximal est de déplacer un marqueur dans une liste. Fusionner les deux aurait fait payer au
canal déclaratif le risque du canal impératif. Le doc de package de `pkg/hook` a été réécrit pour
dire exactement cela : il n'affirme plus « lazyshell n'obéit à personne », il affirme « *ce*
canal-ci n'obéit à personne, l'autre est là, et il est fermé ».

Un socket **par processus lazyshell**, pas par session : la session visée voyage dans la requête, et
`config.RuntimeDir` (extrait de `pkg/hook` pour l'occasion) rappelle qu'un chemin de socket Unix
plafonne autour de 100 octets — un socket par session dépenserait ce budget N fois pour rien.

## Décision 2 — Fermé par défaut, un seul interrupteur, et on dit ce qu'il coûte

`control.enabled` vaut `false`. Tant qu'il vaut `false`, il n'y a **pas de socket du tout** : ni
fichier créé, ni variable `$LAZYSHELL_CONTROL_SOCK` injectée dans les sessions. L'absence de la
variable est précisément la manière dont une session apprend que la fonctionnalité est éteinte.

Ce que l'interrupteur concède, une fois mis à `true`, doit être dit sans l'adoucir :

- **Il n'y a ni jeton ni identification.** Les permissions `0600` du socket sont la seule frontière.
  Donc **tout processus tournant sous le même compte** peut créer des sessions et y taper — pas
  seulement les agents lancés dans lazyshell.
- **`read` rend le scrollback verbatim**, secrets compris. `looksLikeSecret` (onglet env) n'a pas
  d'équivalent ici, et ne peut pas en avoir : un identifiant affiché dans un shell est indiscernable
  de n'importe quel autre texte une fois à l'écran.
- **`new` et `send` sont de l'exécution de commandes**, avec les droits de l'utilisateur.

Trois alternatives ont été pesées et écartées :

1. **Jeton par session** (`$LAZYSHELL_CONTROL_TOKEN` unique, exigé dans la requête). Réduit la
   surface aux processus qui héritent de l'environnement d'une session — un vrai gain. Écarté pour
   la simplicité : c'est un mécanisme d'authentification à écrire, à faire tourner et à documenter,
   là où le dépôt n'en a aucun aujourd'hui. **C'est l'évolution la plus probable si la
   fonctionnalité s'installe** ; le protocole a déjà une place pour un champ.
2. **Confirmation interactive dans le TUI** à chaque verbe mutateur (modèle `direnv` / `trust.go`).
   Sûr, mais détruit le cas d'usage : un agent chef travaille pendant que l'utilisateur regarde
   ailleurs, et une popup bloquante transforme le pilotage en attente.
3. **Opt-in par session.** Granularité intermédiaire, mais un deuxième axe de configuration pour un
   gain qui reste partiel : les sessions autorisées peuvent toujours tout piloter.

## Décision 3 — Ce qui n'est *pas* exposé

- **Pas de `delete`.** `kill` termine le processus et laisse la session listée comme `exited` — la
  sémantique de la touche `x` du TUI, pas celle de `D`. Supprimer définitivement est le seul geste
  sans retour ; un agent n'a rien à y faire.
- **`new` ne vole ni la sélection ni le clavier**, contrairement à la touche `n`
  (`selectNewlyCreatedSession` + `focusSelectedShell`). Ces deux gestes sont justes quand
  l'utilisateur a appuyé sur une touche et hostiles quand c'est un agent en tâche de fond : le
  curseur serait arraché à ce qu'il était en train de taper.
- **`send` n'ajoute pas de retour chariot.** Le texte part verbatim, `--enter` est explicite —
  sémantique de `tmux send-keys` : appuyer sur Entrée est un acte.
- **`ctl` sort en code non nul**, à l'inverse exact de `lazyshell hook` qui renvoie toujours 0. Le
  hook est un effet de bord du tour d'un agent, et lazyshell ne doit jamais être la raison pour
  laquelle ce tour échoue ; un appel `ctl` *est* l'action de l'agent, qui doit savoir qu'elle a
  échoué.

## Décision 4 — La frontière de goroutine est dans `pkg/gui`, et elle est explicite

`pkg/control` ne connaît ni gocui ni `pkg/session` : `control.Handler` est la couture, implémentée
par `*Gui`. Chaque méthode tourne sur une goroutine de connexion, ce qui les partage en deux, et la
règle est écrite en tête de `pkg/gui/control.go` parce qu'elle ne se déduit pas :

- `list`, `read`, `send` ne touchent que des choses déjà protégées par leur propre mutex
  (`session.Manager`, `pkg/screen`, `Session.Write`) et s'exécutent en place ;
- `new` et `rename` passent par `onGUI` — elles dépensent `gui.sessionCounter` ou repeignent le
  panneau, deux choses réservées à la goroutine de gocui ;
- `kill` est **les deux** : `Manager.Kill` attend que le groupe de processus soit effectivement
  récolté (jusqu'à 4 s par défaut) et s'exécute donc en place, seul le repaint traverse.

`onGUI` a un délai de garde (2 s, sous les 3 s de `control.Call`) : une boucle d'évènements bloquée
doit produire une erreur lisible, jamais une connexion suspendue.

Le cas de `kill` mérite d'être retenu, parce qu'il s'est produit : mettre le `Manager.Kill` à
l'intérieur d'`onGUI` semblait cohérent avec `new` et `rename`, et le premier `ctl kill` réel a
répondu « interface occupée : délai dépassé » — la garde de 2 s déclenchée par un kill simplement
*lent*, pas bloqué. La touche `x` du TUI refusait déjà, elle, de faire tourner ce même appel sur la
goroutine de gocui (`killSession` l'enveloppe dans `runBusy`). Deux erreurs symétriques, et elles ne
se manifestent pas de la même façon : du travail réservé au GUI exécuté en place est une course que
les tests n'attrapent pas de façon fiable ; du travail lent exécuté dans `onGUI` gèle l'interface et
fait sauter la garde.

## Conséquences

- Le commentaire de `Session.SetAgentState` — « the one place something outside lazyshell can affect
  a Session » — était devenu faux et a été réécrit : c'est le seul point d'entrée du canal
  *déclaratif*.
- Une nuance héritée, constatée en écrivant les tests et laissée telle quelle :
  `opts.Env` (dernière couche de `buildEnv`) écrase les variables dites « forcées », donc un fichier
  de projet peut rediriger `$LAZYSHELL_CONTROL_SOCK` comme il peut déjà rediriger `$LAZYSHELL_SOCK`.
  Ce n'est pas une brèche nouvelle — approuver un fichier de projet, ce que `trust.go` impose déjà,
  c'est déjà accepter qu'il lance des commandes — mais l'écart entre le commentaire et le
  comportement est épinglé par un test plutôt que laissé à découvrir.
- La ligne « Agent control API : decided against for now » de `CLAUDE.md` est remplacée par un
  renvoi ici.
