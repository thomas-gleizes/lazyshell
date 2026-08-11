# ADR 0009 — Watchers de motifs par session

- **Statut** : accepté.
- **Date** : 2026-08-11
- **Contexte** : `ROADMAP.md` (« Watchers de motifs par session ») demandait de généraliser la
  notification façon agent aux sessions ordinaires, et signalait lui-même le point non tranché :
  l'anti-rebond — un motif qui matche 200 lignes ne doit pas produire 200 notifications. Ce document
  couvre cette question et les décisions qui en découlent côté `pkg/session`/`pkg/config`/`pkg/gui` ;
  il ne touche à aucune décision des ADR 0001–0008.

## Contexte

Un serveur de dev qui casse dans une session cachée ne se signale aujourd'hui que par le marqueur
d'activité générique (`●`) — indistinguable d'une session qui produit simplement de la sortie
normale. `checkAgentNotifications` et `checkCommandExitNotifications`
(`pkg/gui/notify.go`) existent déjà pour deux cas particuliers (état d'agent, code de sortie
OSC 133) ; ce qui manque est le cas général : un motif regex sur n'importe quelle ligne de sortie,
déclaré dans `lazyshell.yml` ou armé à la volée sur la session sélectionnée.

## Décision 1 — Un troisième tap dans `sessionTapWriter`, pas un nouveau chemin de lecture

`pkg/session/session.go`'s `drain` alimente déjà l'émulateur via un `io.Writer` intermédiaire
(`agentEvalWriter`, renommé `sessionTapWriter`) qui déclenche `evaluateAgentState()` après chaque
bloc écrit — précisément pour que `io.Copy(s.screen, s.ptmx)` garde son fast-path (`ptmx.WriteTo`)
sans qu'un second goroutine ne relise `s.screen`. `feedWatch(p)` devient le second effet de bord de
ce même `Write`, sur les mêmes octets, sans toucher à la forme de `drain`.

L'alternative considérée — lire les lignes déjà découpées dans le scrollback de `pkg/screen`
(comme OSC 133 le fait pour ses bornes) — a été écartée : elle coupla la détection de motifs au
fork `vt`, pour un gain nul (`feedWatch` n'a besoin d'aucune des garanties qu'OSC 133 tire de
`Scrollback.Evicted()` — un motif raté par accident à la coupure du tampon ligne n'a pas les mêmes
conséquences qu'un repère de commande qui pointerait au hasard). `feedWatch` découpe donc lui-même
les octets bruts sur `\n`, dans un tampon propre à chaque `Session`, et matche sur
`ansi.Strip(ligne)` — le texte tel qu'affiché, pas les octets d'échappement qui l'entourent.

Un tampon qui ne voit jamais de `\n` (un spinner piloté par `\r`, un flux binaire) est plafonné à
`watchLineBufMax` (4096 octets) : passé ce seuil, ce qui a été accumulé est matché tel quel et le
tampon est vidé — même réflexe défensif que `agentScreenTailLines`.

## Décision 2 — La porte d'écran alterné, réutilisée telle quelle

Un `vim` ou un `htop` en écran alterné peut écrire n'importe quoi, y compris des séquences qui
ressembleraient par accident à une ligne de log. `feedWatch` ne matche donc rien tant que
`Screen.IsAltScreen()` répond vrai, et vide son tampon à chaque transition (dans les deux sens) :
une ligne partielle accumulée juste avant de basculer ne doit jamais se retrouver recousue à des
octets de l'autre côté. C'est exactement le principe que la Décision 3 de l'ADR 0008 a posé pour
les bornes OSC 133 (« signaler l'état, jamais changer de comportement de sa propre initiative »),
appliqué ici au même problème plutôt que réinventé.

## Décision 3 — Anti-rebond : un plafond par motif, pas de re-vérification différée

Chaque motif armé (déclaré ou posé à la volée) porte son propre `lastFired`. Une correspondance
survenant moins de `watchCooldown` (3 secondes) après la précédente est silencieusement ignorée —
ni mise en file, ni coalescée. C'est la réponse directe à la question que `ROADMAP.md` posait.

Contrairement à `evaluateAgentState`, qui arme un `time.AfterFunc` pour rattraper un état final
manqué pendant la fenêtre de throttle (un agent qui passe `blocked` juste avant de se taire ne doit
jamais rester bloqué sans notification), un watcher n'a pas d'« état final » à rattraper : la
prochaine ligne qui matche réellement, une fois la fenêtre rouverte, suffit. Ajouter la même
mécanique de rattrapage ici aurait résolu un problème que ce mécanisme n'a pas.

## Décision 4 — Livraison par sondage, pas par callback

`Session.LastWatchHit() (WatchHit, bool)` expose le dernier match éligible à notification, avec un
`Seq` monotone — même forme que `Screen.LastCommandExit()`. `checkWatchNotifications`
(`pkg/gui/notify.go`) le sonde sur le même tick que `checkAgentNotifications` et
`checkCommandExitNotifications`, et compare le `Seq` vu la dernière fois pour ne notifier qu'une
fois par match. Aucun nouveau mécanisme de notification : `oscNotifyWrite`/`runNotifyFallback`
sont réutilisées telles quelles.

## Décision 5 — Schéma de fichier de projet : `WatchSpec` suit `GroupSpec`

`SessionSpec.Watch []WatchSpec` (`pattern`, `notify`) suit la doctrine de liste blanche déjà posée
par ce fichier : décrire ce qui existe, jamais l'apparence de l'interface — exactement le
raisonnement qui a délibérément gardé `GroupSpec` à un seul champ `Name`. `Validate()` compile
chaque motif et écarte (en le signalant) celui qui échoue, au même grain qu'un `groups:` invalide :
une seule entrée mauvaise ne coûte que ce watcher-là, jamais la session ni les autres watchers
déclarés à côté. `pkg/session` porte son propre petit `WatchSpec` plutôt que d'importer
`pkg/config` — même séparation que `ResolvedSession` applique déjà pour tous ses autres champs.

## Décision 6 — La touche `v` efface plutôt que de ne rien faire sur une soumission vide

`renameSession` ignore une soumission vide (un nom ne doit jamais être vide). `armWatchPattern`
fait l'inverse : soumettre une entrée vide désarme le motif posé à la volée. « Aucun motif armé »
est un état normal et utile pour une session, contrairement à « aucun nom » — et vider le prompt
est le moyen le plus découvrable d'y revenir. Les watchers déclarés dans `lazyshell.yml` ne sont
jamais concernés par cette touche : elle n'agit que sur le motif unique, remplaçable, posé à la
volée sur la session sélectionnée.

## Conséquences

- Une clé de fichier de projet de plus (`sessions[].watch`), une clé i18n de plus
  (`notify.watch_match`, plus `action.arm_watch` et `prompt.watch_pattern`), une action remappable
  de plus (`arm_watch`, touche `v`) — non dupliquée dans `editDuringScroll` : contrairement à
  `zoom`/aux touches de tabulation/aux sauts de prompt, elle n'a de sens que depuis la liste des
  sessions (`hasSelectedSession`), jamais depuis le panneau de sortie en défilement.
- `pkg/session` gagne une dépendance directe à `github.com/charmbracelet/x/ansi` pour
  `ansi.Strip` — déjà une dépendance transitive du module via `pkg/screen`, donc rien de nouveau
  dans `go.mod`.
