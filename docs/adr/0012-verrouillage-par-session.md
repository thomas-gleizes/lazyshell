# ADR 0012 — Le verrouillage devient un état par session

- **Statut** : accepté.
- **Date** : 2026-08-13
- **Contexte** : amende la **décision 1** de l'**ADR 0011** (« persistance pure d'un drapeau
  global »). Les décisions 2 (le mode verrouillé garde toutes ses actions) et 3 (couleurs et
  `borderColorFor`) de l'ADR 0011 sont inchangées, de même que l'automate de sortie des ADR 0004
  et 0005.

## Contexte

L'ADR 0011 a fait du pass-through l'état par défaut et a tranché, pour le changement de sélection,
en faveur de la persistance pure : `passThroughActive` est un drapeau global que `j`/`k`, un clic ou
la molette ne touchent jamais. C'était le bon arbitrage tant que toutes les sessions se
ressemblaient — des shells dans lesquels on tape.

Elles ne se ressemblent pas. Une session déclarée dans un `lazyshell.yml` avec `command: npm run
dev` n'est pas une session dans laquelle on tape : c'est une session qu'on regarde. Y arriver
déverrouillé, ce qui est désormais le cas normal, veut dire qu'une frappe parasite — un `Ctrl-C`,
un `q` tapé pour une autre vue, une lettre — part droit dans la commande en cours et la tue. Le
coût d'une erreur est asymétrique : à gauche, une frappe perdue qu'il faut retaper ; à droite, un
serveur de dev tué au milieu d'autre chose.

## Décision 1 — L'état verrouillé/pass-through est mémorisé par session

`passThroughActive` reste le drapeau unique que lisent l'éditeur, la bordure, la barre de statut et
la tâche de rendu — rien de tout cela ne change. Ce qui change est qu'il porte désormais l'état de
la session *courante*, et qu'une seconde structure le mémorise :
`Gui.lockedBySession map[string]bool`, à **entrées explicites uniquement**, clé = id de session.

- **Entrée présente** : la sélectionner applique son état (`applyLockState`, appelé par
  `onSelectionChanged` avant `showOutput` — la tâche de rendu capture le drapeau, il doit donc être
  posé avant elle).
- **Entrée absente** : le drapeau courant est conservé tel quel, c'est-à-dire exactement la
  persistance de l'ADR 0011. Elle reste donc la règle pour tout ce que personne n'a jamais tranché :
  sessions créées à la main (`n`/`N`/`M`/`c`), sessions créées par `ctl new`, sessions d'un projet
  sans `locked:` ni commande.

Une entrée n'est écrite que par deux choses : le démarrage des sessions déclarées (décision 2) et un
geste **explicite** de l'utilisateur sur la session sélectionnée (décision 3).

L'alternative — stocker l'état dans `session.Session` — a été écartée : le verrouillage est une
propriété de l'interface, pas du processus, et `pkg/session` ne connaît rien de `pkg/gui`. La map
côté GUI, alimentée au démarrage par `SetLockedSessions` comme `SetGroupOrder` l'est pour les
groupes, garde ce partage intact. Elle est indexée sur l'id de session, que `Manager.Restart`
conserve : un `R` sur une session verrouillée la retrouve verrouillée, pas remise à zéro.

## Décision 2 — `locked:` dans le fichier projet, et « une commande déclarée démarre verrouillée »

`SessionSpec` gagne `Locked *bool` (`yaml:"locked"`), pointeur pour distinguer « déclaré à faux » de
« non déclaré », même convention que `NoDefaultEnv`. `Validate` le résout en un `bool` franc porté
par `ResolvedSession.Locked` (`resolveLocked`), selon une règle unique :

> valeur déclarée si elle existe, sinon **verrouillé si et seulement si la session déclare un
> `command:`**.

L'heuristique s'arrête aux sessions déclarées. Une session créée à la main ou par `ctl new
--command` continue d'arriver en pass-through, parce que `focusSelectedShell` l'arme sans condition
et que c'est correct : on ne crée pas une session pour la regarder (ADR 0011). Étendre l'heuristique
à ces chemins aurait demandé de contredire `focusSelectedShell`, pour un cas où l'utilisateur vient
justement d'agir.

`locked:` passe la whitelist du fichier projet alors qu'elle refuse tout ce qui touche à
l'interface (ADR 0007, doctrine de `ProjectConfig`). C'est assumé, pour deux raisons : ce que la clé
protège est le processus déclaré lui-même, pas l'apparence de la vue ; et le pire qu'une valeur
hostile puisse faire est d'obliger l'utilisateur à appuyer sur `i`. Elle ne dit rien d'une couleur,
d'une touche ou d'une disposition, et reste donc du même ordre que `watch:` ou `restart:` — ce qui
existe, pas ce à quoi ça ressemble. Aucune clé n'est ajoutée à la config *utilisateur* : il n'y a
pas de réglage global du défaut, l'heuristique est câblée.

## Décision 3 — Seul un geste utilisateur est mémorisé

`exitPassThrough` (touche préfixe, paire `Échap` `Échap`) et `enterPassThrough` (`i`/`Entrée`,
`focusSelectedShell`) écrivent l'entrée de la session sélectionnée. Les verrouillages **techniques**
ne l'écrivent pas et passent par une nouvelle fonction `lockOutput`, qui fait tout ce que faisait
`exitPassThrough` sauf mémoriser :

- `setTab` (`pkg/gui/tabs.go`) verrouille en quittant l'onglet `terminal`. Mémoriser ici épinglerait
  la session en verrouillé après un simple aller-retour par l'onglet `perf` — un effet de bord que
  l'utilisateur n'a pas demandé et ne pourrait pas relier à sa cause.
- `backOutOfExitedSession` (`pkg/gui/exit_watch.go`) verrouille quand le shell se termine. Ce n'est
  pas une préférence mais un constat sur un processus mort, et comme `Restart` réutilise l'id, le
  mémoriser ferait revenir la session redémarrée en verrouillé alors que `focusSelectedShell` vient
  d'y donner le clavier.

C'est la règle la plus facile à casser du lot : tout nouvel appelant qui verrouille doit choisir
entre `exitPassThrough` (geste, mémorisé) et `lockOutput` (technique, non mémorisé), et le défaut
correct pour du code non déclenché par une frappe est `lockOutput`.

Ordre d'appel notable : `enterPassThrough` mémorise **avant** d'appeler `onSelectionChanged`, sans
quoi `applyLockState` re-verrouillerait aussitôt la session qu'on vient de déverrouiller.

Enfin, `applyLockState` restaure un verrouillage sans condition mais reprend les deux gardes de
`enterPassThrough` pour restaurer un pass-through : pas sur un onglet autre que `terminal`
(`editOutput` teste `passThroughActive` avant l'onglet actif, on taperait dans un rapport statique)
et pas sur un shell terminé. Une mémorisation ne doit pas être un chemin détourné vers un état que
le geste équivalent refuse.

## Conséquences

- `onSelectionChanged` appelle `refreshChrome` en fin de course : la bordure (`borderColorFor`) et
  le hint `status.locked_hint` de l'ADR 0011 suivent le changement d'état sans code nouveau.
- `deleteSession` oublie l'état mémorisé (`forgetLockState`), à côté de `forgetPerfHistory` et pour
  la même raison. Tuer une session ne l'oublie pas : elle reste listée, et redémarrable.
- `autostart` (`pkg/app/project.go`) renvoie désormais la map id → verrouillé en plus de ses
  erreurs ; c'est le seul endroit où une `ResolvedSession` et la `*session.Session` qu'elle a
  produite coexistent.

## Ce qui ne change pas

- Les décisions 2 et 3 de l'ADR 0011, `editDuringPassThrough`, `editDuringScroll`, `escExitWindow`,
  la résolution de `prefixKey`/`$LAZYSHELL_PREFIX`.
- `pkg/session` ignore toujours ce qu'est un verrouillage : rien n'est ajouté à `session.Options`.
- La config utilisateur : aucune clé nouvelle, donc rien à documenter dans le tableau `### Reference`
  du README ni à faire passer par `pkg/config/doc_test.go`.
