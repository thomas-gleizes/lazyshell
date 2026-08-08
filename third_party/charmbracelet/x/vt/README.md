# Fork local : github.com/charmbracelet/x/vt

Copie locale de la version `v0.0.0-20260803091719-3755ebad01b1` de
`github.com/charmbracelet/x/vt`, avec un seul correctif appliqué dans
`emulator.go` : le champ `Emulator.closed` est passé de `bool` à
`atomic.Bool`.

## Pourquoi

`pkg/screen.Screen.Close()` doit pouvoir débloquer un goroutine parké dans
`Screen.Read()` — c'est le seul moyen de le faire, et c'est testé
explicitement par `TestCloseUnblocksRead`. Cet appel concurrent à
`Read()`/`Close()` est un pattern voulu côté lazyshell, mais l'implémentation
amont de `Emulator.Read()`/`Emulator.Close()` lit et écrit `e.closed` (un
`bool` nu) sans aucune synchronisation, ce qui fait échouer systématiquement
`go test -race` — et ce depuis le tout premier commit qui a introduit
`Read()`/`Close()` sur `Emulator` (`0e720abcae8b`, 2025-09-11) ; il n'existe
donc aucune version en amont, ancienne ou récente, sans cette race.

## Comment retirer ce fork

Dès qu'un correctif équivalent est mergé en amont (ou dans une nouvelle
version taguée), supprimer ce dossier et la ligne `replace` correspondante
dans `go.mod` à la racine du dépôt.
