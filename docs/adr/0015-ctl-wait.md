# ADR 0015 — `ctl wait` : bloquer sur un état plutôt que le sonder

- **Statut** : accepté.
- **Date** : 2026-08-20
- **Contexte** : point 2 de `ROADMAP.md` (« `ctl wait` — attendre un état au lieu de le sonder ») :
  un agent orchestrateur qui veut savoir qu'une session est passée `blocked` doit aujourd'hui
  boucler sur `ctl list`. Ce document couvre l'ajout du verbe `wait`, et en particulier la première
  vraie rupture du protocole `pkg/control` : jusqu'ici chaque verbe répond immédiatement, `wait`
  bloque volontairement jusqu'à plusieurs minutes. Il ne touche à aucune décision des ADR 0001–0014.

## Contexte

`pkg/control`'s propre commentaire de paquet, le commentaire de `callTimeout` et celui de
`pkg/gui/control.go` sur le partage inline/`onGUI`/scindé affirment tous les trois, chacun à sa
façon, la même hypothèse implicite : une requête reçoit sa réponse tout de suite. `wait` la casse
délibérément — c'est son but — et cette ADR existe pour rendre explicite ce que cette rupture
implique à chacun des trois endroits, plutôt que de laisser un futur lecteur la déduire d'un
commentaire qui ne parlait pas encore de ce cas.

Ceci ne rouvre pas le modèle de sécurité de l'ADR 0006 : aucun jeton, aucune capacité nouvelle
qu'un agent n'avait pas déjà en sondant `ctl list` en boucle, `control.enabled` toujours à `false`
par défaut, permissions `0600` toujours l'unique contrôle d'accès. `wait` est un verbe de plus sur
un socket qui existe déjà, pas un nouvel axe de risque.

## Décision 1 — `Wait` tourne inline, jamais par `onGUI`

`onGUI`'s garde de 2 s existe pour une seule raison : détecter que la boucle gocui est bloquée ou
absente, pas pour borner un travail légitimement long. Un `wait` qui bloque plusieurs minutes ferait
échouer cette garde à chaque appel si on l'y faisait passer, pour la mauvaise raison — exactement le
type d'erreur que l'incident `Kill` (voir `pkg/gui/control.go`, et Décision 4 de l'ADR 0006) a déjà
révélé une fois pour un travail *lent*, pas *bloqué*.

`Wait` ne touche donc jamais rien qui appartienne à gocui : il sonde uniquement
`Session.AgentState()` et `Session.Status()`, deux accesseurs déjà protégés par leur propre mutex et
sûrs depuis n'importe quelle goroutine — la même garantie que `List`/`Read`/`Send`/`GroupSend`
exploitent déjà pour tourner inline. La boucle de sondage (`pkg/gui/control.go`, `waitPollInterval`,
200 ms) reprend directement la forme de `pkg/session/stop_on_failure.go`'s
`watchStopOnFailure` : ni l'un ni l'autre n'a de canal à écouter pour « l'état a changé », donc les
deux sondent, à la même cadence, plutôt que d'en inventer un.

Deux sous-cas :

- Une session unique attend aussi sur `sess.Done()`, qui se ferme dès que le processus est
  réellement récupéré — la sortie est alors détectée immédiatement plutôt qu'au prochain tick, et
  traitée comme une erreur si l'état voulu n'a jamais été atteint.
- Un groupe résout ses membres une seule fois via `resolveGroup` (même précédent que
  `GroupSend`/`GroupKill` : un instantané, pas un recalcul à chaque tick, et jamais l'ordre
  d'affichage du groupe — un concept de rendu seulement, Décision 2 de l'ADR 0007). Un membre qui se
  termine en cours d'attente est le cas ordinaire, pas un échec : `Wait` n'abandonne que lorsque
  *tous* les membres sont sortis sans qu'aucun n'ait jamais atteint l'état.

## Décision 2 — Le délai côté client ne doit jamais être celui qui se déclenche réellement

`pkg/control/client.go`'s `callTimeout` (3 s) borne un appel `ctl` de bout en bout, dimensionné pour
`VerbNew`/`VerbKill`, pas pour une attente de plusieurs minutes. Sans changement, tout `wait` de
plus de quelques secondes se ferait couper au niveau transport — un `i/o timeout` brut — avant même
que le serveur ait eu la moindre chance de répondre avec un `Response{OK:false}` structuré. Un
timeout de `wait` qui a réellement expiré doit être ce second cas, pas le premier : c'est la
différence entre `ctl` qui explique pourquoi il a échoué et `ctl` qui échoue de façon opaque pour la
même raison réelle.

`Call` calcule donc son délai via `callDeadline(req)` : `callTimeout` inchangé pour tous les autres
verbes, mais `resolveWaitTimeout(req.Timeout) + waitDeadlineMargin` (5 s) pour `VerbWait` — une
marge délibérément confortable au-dessus de la cadence de sondage du serveur, pour que celui-ci ait
toujours le temps de constater sa propre échéance et de répondre avant que celle du client ne
morde. Côté serveur, `serveConn` ne pose aucune échéance sur la connexion acceptée : rien n'y force
de changement, un `Handler.Wait` qui bloque longtemps est déjà une forme que le protocole supportait
sans le savoir.

## Décision 3 — Pas de nouveau verbe pour l'authentification, pas de nouvel ADR sur ce point

Répété ici pour qu'un futur lecteur n'ait pas à le déduire : `wait` n'ajoute ni jeton, ni
confirmation interactive, ni opt-in par session. C'est exactement le renoncement déjà acté par
l'ADR 0006 (Décision 2, alternatives rejetées) — un `wait` mal utilisé ne révèle rien qu'un
`ctl list` en boucle ne révélait déjà, juste avec moins de bruit sur le socket.

## Conséquences

- `pkg/control.Request` gagne deux champs (`State`, `Timeout`), ignorés par tous les autres verbes —
  même doctrine que `Tail`.
- `pkg/control.Handler` gagne `Wait`, ce qui force son unique implémentation (`*gui.Gui`) à le
  fournir — la preuve de compilation `var _ control.Handler = (*Gui)(nil)` (`pkg/gui/control_test.go`)
  fait ce travail.
- `ctl wait` peut échouer pour quatre raisons distinctes (session/groupe inconnu, état imparsable,
  délai expiré, session cible sortie avant d'atteindre l'état) — toutes remontent comme
  `Response{OK:false}`, donc une sortie non nulle de `ctl`, cohérent avec le reste du verbe set.
- `DefaultWaitTimeout` (120 s) s'applique quand `--timeout` est omis ou `<= 0` ; aucun mode
  « illimité » n'est exposé, par le même raisonnement que `callTimeout`'s propre commentaire.
