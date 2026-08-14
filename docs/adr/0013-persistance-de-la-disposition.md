# ADR 0013 — Persistance de la disposition entre deux lancements

- **Statut** : accepté.
- **Date** : 2026-08-14
- **Contexte** : ROADMAP.md, point 1 (« Persistance de la disposition entre deux lancements »).
  N'amende aucun ADR existant ; ADR 0007 (groupes) et ADR 0012 (verrouillage par session) sont cités
  comme précédent de style, pas comme décisions modifiées.

## Contexte

Le mode démon reste hors scope (les pty ne survivent pas au process lazyshell), mais la *recette*
d'une session — nom, groupe, répertoire, commande — peut survivre à sa fermeture. Sans
`lazyshell.yml`, chaque lancement repartait de zéro : une seule session par défaut (`session-1`),
sans aucun moyen de retrouver la disposition d'une session de travail précédente.

Deux questions étaient ouvertes dans le roadmap : comment ce nouvel état interagit avec un
`lazyshell.yml` présent, et si la restauration doit être proposée ou automatique.

## Décision 1 — La restauration n'est offerte qu'en l'absence de `lazyshell.yml`

Un fichier projet est une déclaration explicite, versionnée, passée par le magasin de confiance
(`pkg/config/trust.go`) — voir ADR sur le magasin de confiance dans le rapport d'analyse historique.
L'état sauvegardé est un enregistrement implicite de ce qui tournait la dernière fois. Les fusionner
(l'état complète ou remplace les sessions déclarées) reviendrait à laisser une commande obsolète
écraser silencieusement une commande déclarée — une régression de correction, pas un confort.

Concrètement : `pkg/app.newApp` ne consulte `config.LoadState` que quand `pcfg.Path == ""` (aucun
fichier projet trouvé par `config.ProjectPath`). Un `lazyshell.yml` présent, même déclarant zéro
session, garde exactement le comportement d'avant cet ADR — l'état sauvegardé n'est jamais lu dans
ce cas.

En revanche l'état est **toujours réécrit** à la sortie (`App.snapshotState`, `pkg/app/app.go`),
qu'un `lazyshell.yml` ait piloté ce lancement ou non : coût nul, et la dernière disposition réelle
reste récupérable si le fichier projet disparaît un jour.

## Décision 2 — `restore_layout: ask | always | never`, popup par défaut

`Config.RestoreLayout` (`pkg/config/config.go`), chaîne à trois valeurs validées comme
`Config.Language` (`pkg/config/validate.go`) — une valeur inconnue retombe sur `ask` avec un
avertissement plutôt que de faire échouer le chargement :

- `ask` (défaut) : popup de confirmation au lancement, listant les sessions proposées.
- `always` : restauration silencieuse, aucune popup.
- `never` : jamais proposée ; l'état continue d'être écrit à la sortie (décision 1), simplement
  jamais lu.

Un booléen simple a été écarté : il aurait fallu perdre soit le mode automatique silencieux, soit le
mode popup, alors que les deux répondent à un besoin distinct (une machine partagée où l'on veut
être prévenu, vs. une machine personnelle où l'on veut retrouver son poste de travail sans un `y` à
taper à chaque lancement).

### Pourquoi `always` et `ask` ne se ressemblent pas dans le code

`always` restaure de façon synchrone dans `pkg/app.newApp`, avant que le terminal ne soit pris par
gocui — exactement comme `autostart` restaure les sessions d'un fichier projet. `ask` ne le peut
pas : la popup de confirmation (`gui.showConfirm`, réutilisée telle quelle) a besoin d'un
`*gocui.Gui` déjà initialisé (elle se dimensionne depuis `g.Size()`). `newApp` renvoie donc un état
« en attente » (`Gui.pendingRestore`/`restoreShell`, posés par `SetPendingRestore` avant `Run`, même
contrat que `SetStartupError`), et `Gui.Run` le consomme en mettant en file une confirmation
(`g.Update`) juste avant `MainLoop` — après le premier `SetManager`, comme le fait déjà le serveur de
contrôle (`startControlServer`) pour la même raison de séquencement.

Un refus (`n`/`Échap`) ne crée aucune session de repli : la liste reste aussi vide qu'avec
`--no-autostart`, et `n` est à portée de main. Ajouter un filet de sécurité aurait dupliqué la
branche « aucune session déclarée » pour un cas que l'utilisateur vient de refuser explicitement.

## Format du fichier d'état

`pkg/config/state.go` :

```go
type StateFile struct {
    Path     string
    SavedAt  time.Time
    Sessions []StateSession
}

type StateSession struct {
    Name, Group, Cwd, Command string
}
```

Délibérément plus étroit que `SessionSpec` : `Env`, `EnvFiles`, `Watch`, `Restart`, `Locked` sont des
concepts de déclaration de projet qui passent par le magasin de confiance, hors du périmètre
« nom + cwd + command + groupe » que le roadmap fixait. `pkg/session` et `SessionSpec`/
`ResolvedSession` (`pkg/config/project.go`) restent inchangés — la seule addition côté session est un
accesseur pur, `Session.Command()`, `opts.Command` étant déjà immuable après création (`Restart`
recopie `old.opts`, ne le modifie jamais).

**Chemin** : `~/.config/lazyshell/state/<sha256 hex du cwd absolu>.yml` (`StatePath`, `configDir()` +
`state/`) — le nom de fichier suit le texte du roadmap (« hash-du-cwd ») pour éviter toute collision
entre projets ; le cwd d'origine est aussi stocké en clair dans le champ `path:` pour rester
inspectable malgré le nom de fichier opaque.

**Permissions** : `SaveState` écrit par fichier temporaire + `rename` sous le même répertoire, mode
`0600` posé avant tout contenu — même forme atomique que `Trust` pour `trust.yml`
(`pkg/config/trust.go`), et pour la même raison : un fichier lu par erreur par un autre processus,
ou partiellement écrit, ne doit jamais faire tourner une commande. `LoadState` ignore silencieusement
(retourne `nil, nil`, comme un fichier absent) un fichier dont les permissions dépassent `0600` — pas
de nouveau mécanisme de confiance : ce fichier n'a jamais besoin d'une approbation, puisqu'il ne
contient que des commandes ayant déjà tourné une fois sous ce même compte ; son seul risque est
qu'un autre processus l'ait modifié après coup, ce que le contrôle de permissions suffit à écarter.

## Ce qui ne change pas

- `pkg/session` : aucun champ, aucune sémantique de cycle de vie nouvelle — `Command()` est un
  accesseur pur sur une valeur déjà immuable.
- `SessionSpec`/`ResolvedSession` (`pkg/config/project.go`) : inchangés.
- Comportement d'un `lazyshell.yml` présent (`autostart`, `pkg/app/project.go`) : identique en tout
  point, y compris quand il déclare zéro session.
- `pkg/config/trust.go` : aucune entrée, aucun mécanisme nouveau pour le fichier d'état.
- Aucun process persistant : `lazyshell` continue de tout tuer à la sortie (`Manager.Shutdown`) ;
  seul le YAML de `~/.config/lazyshell/state/` survit.
