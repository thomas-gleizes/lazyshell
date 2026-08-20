# ADR 0014 — Arrêt de session sur échec de la commande

- **Statut** : accepté.
- **Date** : 2026-08-20
- **Contexte** : demande utilisateur directe (pas une entrée `ROADMAP.md` préexistante) : `command:`
  reste tapée dans le shell et laisse celui-ci ouvert quel que soit le code de sortie (voir la
  Décision 5 de l'ADR 0010 sur `restart:`, qui traite déjà le cas inverse — relancer — mais rien
  n'existe pour arrêter). Ce document couvre l'ajout d'un opt-in par session,
  `stop_on_failure:`, et son interaction avec la supervision de redémarrage déjà actée par
  l'ADR 0010. Il ne touche à aucune décision des ADR 0001–0013.

## Contexte

Un `SessionSpec` peut déclarer `command: make dev` : la commande est tapée dans le shell une fois
qu'il est prêt, pas exécutée à sa place (`pkg/session/manager.go`, doc-comment de `newSession`) —
donc quand elle se termine, en succès ou en échec, le shell reste ouvert en dessous. C'est le
comportement voulu pour un `npm run dev` qu'on relance à la main, mais rien ne permet l'inverse :
un utilisateur qui veut qu'un échec de la commande arrête *directement* la session, au lieu de
laisser un shell inerte ouvert, doit le faire lui-même.

## Décision 1 — Un `bool` simple, pas un pointeur

`SessionSpec.StopOnFailure bool` (`yaml:"stop_on_failure"`), et son pendant résolu
`ResolvedSession.StopOnFailure bool`. Contrairement à `Locked *bool` (ADR 0012), qui a besoin de
distinguer « non déclaré » d'un défaut hérité d'un autre champ (la présence de `command:`),
`stop_on_failure` non déclaré et déclaré `false` sont exactement le même comportement : le statu
quo. Il n'y a pas de deuxième axe à faire porter par un pointeur, donc la valeur zéro de Go suffit
— même raisonnement que `Restart string`/`RestartPolicy` (ADR 0010, Décision 1).

Une session qui déclare `stop_on_failure: true` sans `command:` est acceptée mais inerte (il n'y a
rien à surveiller) ; `warnIfStopOnFailureIsInert` le signale sans faire tomber la session, le même
traitement « valeur inoffensive mais probablement une erreur d'auteur » que `resolveRestartPolicy`
réserve à une politique inconnue.

## Décision 2 — Sondage interne à `pkg/session`, pas une vérification sur le tick GUI

`Screen.LastCommandExit()` (`pkg/screen/osc133.go`, ADR 0008) est délibérément *poll-only* :
`commitCommand` ne notifie personne quand un `D` arrive, elle se contente d'incrémenter un
compteur de séquence que l'appelant doit sonder. Les deux consommateurs existants de cet
accesseur (`pkg/gui/notify.go` pour le marqueur `command_failed`, et `Session.LastWatchHit`
pour les watchers de motifs, ADR 0009) le font depuis le tick de rendu de `pkg/gui`.

Ce n'est pas praticable ici : `pkg/session` ne doit pas dépendre de `pkg/gui` (voir la doctrine de
découplage des paquets de `CLAUDE.md`), et une session de fichier projet est créée par
`pkg/app`'s `autostart()` avant même que `gui.Run` démarre sa propre boucle de tick — une
dépendance au tick GUI ferait manquer une commande qui échoue instantanément au lancement, avant
que quoi que ce soit côté GUI ne tourne. `watchStopOnFailure` (`pkg/session/stop_on_failure.go`)
est donc une goroutine de sondage interne à `pkg/session`, sur le modèle de `supervise`
(`restart.go`) — sauf que `supervise` attend `<-sess.Done()` (le process a déjà fini), alors que
cette fonctionnalité doit agir *pendant* que le process est encore vivant, ce qui exclut d'attendre
sur ce canal seul.

## Décision 3 — Ne réagit qu'au tout premier événement de fin de commande de l'incarnation

`watchStopOnFailure` s'arrête dès que `seq == 1` sur `LastCommandExit()` — pas seulement `ok`.
`seq` part de 0 et s'incrémente à chaque `D`, et chaque incarnation reçoit un `*screen.Screen`
neuf (`m.newScreen` dans `newSession`), donc `seq == 1` désigne sans ambiguïté le tout premier
événement que cette incarnation ait jamais rapporté. Puisque `opts.Command` est injecté comme la
toute première chose tapée dans le shell fraîchement créé, c'est forcément la commande déclarée,
jamais une commande que l'utilisateur tape ensuite à la main dans ce même shell encore vivant.
Une fois cet événement observé — qu'il ait déclenché un arrêt ou non — la goroutine n'a plus rien
à surveiller et se termine ; elle ne se réarme jamais.

Un `D` sans code de sortie explicite (`hasCode == false`) est traité comme « inconnu », pas comme
« échec » : choix conservateur assumé, pour ne jamais tuer une session sur la foi d'une
information que le shell n'a pas transmise.

## Décision 4 — Passe toujours par `Manager.Kill`, jamais `Session.Kill` directement

`Session.Kill` pose `killedExplicitly = true` avant tout le reste ; `WillAutoRestart()`
(`restart.go`) renvoie `false` dès que ce indicateur est posé, quel que soit le code de sortie ou
la politique. `Manager.Kill` est le seul chemin qui passe à la fois par `cancelSupervision` et
`Session.Kill` — c'est ce double effet qui garantit qu'une session `stop_on_failure: true` +
`restart: on-failure` ne se relance pas toute seule immédiatement après avoir été tuée.
`watchStopOnFailure` appelle donc systématiquement `m.Kill(id)`, jamais `sess.Kill(...)` : le même
principe que la Décision 5 de l'ADR 0010, « un arrêt explicite l'emporte toujours sur la
politique » — ici l'arrêt explicite est déclenché par la commande elle-même plutôt que par une
touche, mais la garantie recherchée est identique.

La revérification `m.sessions[id] != sess` juste avant `m.Kill(id)` est la même optimisation que
celle de `fireAutoRestart` — elle referme une course étroite, elle n'est pas à elle seule la
garantie de correction : `sess` ne peut être remplacée tant qu'elle tourne encore (`Manager.Restart`
exige `StatusExited`), donc la seule fenêtre où elle deviendrait périmée est un croisement très
étroit avec une sortie naturelle de `sess` suivie d'un redémarrage automatique très rapide.

## Conséquences

- Une clé de fichier de projet de plus (`sessions[].stop_on_failure`).
- Dépend de la même limite déjà actée par l'ADR 0008 : un shell sans intégration OSC 133 configurée
  n'émet jamais de `D`, donc `stop_on_failure` ne se déclenche silencieusement jamais pour un tel
  shell — pas une régression introduite ici, une dépendance déjà partagée avec
  `markers.command_failed`.
- `lazyshell ctl new` ne gagne pas de moyen de poser `stop_on_failure` — même exclusion que celle
  déjà actée pour `restart` (ADR 0010, Conséquences), étendue par analogie.
- `StopOnFailurePollInterval` est une constante fixe dans cette itération, seulement exposée comme
  champ de `Manager` pour que les tests puissent la raccourcir — pas de réglage possible depuis le
  fichier de projet ou la config utilisateur, même traitement que `RestartBackoffBase`/
  `RestartSuccessDuration`.
