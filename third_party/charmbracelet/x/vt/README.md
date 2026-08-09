# Fork local : github.com/charmbracelet/x/vt

Copie locale de la version `v0.0.0-20260803091719-3755ebad01b1` de
`github.com/charmbracelet/x/vt`, avec deux correctifs appliqués.

## Correctif 1 : `Emulator.closed` en `atomic.Bool` (`emulator.go`)

`pkg/screen.Screen.Close()` doit pouvoir débloquer un goroutine parké dans
`Screen.Read()` — c'est le seul moyen de le faire, et c'est testé
explicitement par `TestCloseUnblocksRead`. Cet appel concurrent à
`Read()`/`Close()` est un pattern voulu côté lazyshell, mais l'implémentation
amont de `Emulator.Read()`/`Emulator.Close()` lit et écrit `e.closed` (un
`bool` nu) sans aucune synchronisation, ce qui fait échouer systématiquement
`go test -race` — et ce depuis le tout premier commit qui a introduit
`Read()`/`Close()` sur `Emulator` (`0e720abcae8b`, 2025-09-11) ; il n'existe
donc aucune version en amont, ancienne ou récente, sans cette race.

**Retrait** : dès qu'un correctif équivalent est mergé en amont (ou dans une
nouvelle version taguée), ce correctif peut disparaître.

## Correctif 2 : `Scrollback.Evicted()` (`scrollback.go`)

`Scrollback` est une simple tranche (`[]uv.Line` + `maxLines`) : `Push` et
`SetMaxLines` évincent les lignes les plus anciennes sans jamais prévenir
l'appelant, et une position dans le tampon (l'« index absolu » que `pkg/screen`
utilise déjà pour `Find`/`RenderAt`/`TextRange`) change donc de sens de façon
silencieuse à chaque éviction.

`pkg/screen`'s support OSC 133 (`docs/adr/0008-integration-shell-osc-133.md`)
a besoin de repères (début de prompt, plage de sortie d'une commande) qui
restent valides après que le scrollback a débordé sa taille configurée
(`scrollback_size`, 10000 lignes par défaut). Le correctif ajoute un compteur
`evicted` sur `Scrollback`, incrémenté à chaque ligne évincée par `Push`,
`SetMaxLines` ou `Clear`, et exposé par `Evicted() int`. Un appelant peut
alors calculer un identifiant monotone (`Evicted() + position` au moment où
il observe la ligne) qui garde son sens indéfiniment : le retraduire en
position courante est `id - Evicted()`, un résultat négatif signifiant que la
ligne visée a depuis été évincée.

**Retrait** : contrairement au correctif 1, celui-ci n'attend pas un correctif
de course amont — rien n'indique qu'un tel compteur ait vocation à exister en
amont, puisqu'il ne sert qu'au besoin propre à lazyshell décrit ci-dessus. Il
peut rester indéfiniment, indépendamment du sort du correctif 1.

## Comment retirer ce fork

Le correctif 1 disparaît dès qu'un correctif équivalent est mergé en amont
(ou dans une nouvelle version taguée) — le correctif 2 doit alors être
réappliqué à la nouvelle version avant de supprimer ce dossier et la ligne
`replace` correspondante dans `go.mod` à la racine du dépôt.
