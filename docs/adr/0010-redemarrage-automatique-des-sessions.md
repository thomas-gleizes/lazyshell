# ADR 0010 — Redémarrage automatique des sessions

- **Statut** : accepté.
- **Date** : 2026-08-11
- **Contexte** : `ROADMAP.md` (« `restart: on-failure` dans le fichier projet ») demandait une
  politique de redémarrage par session, et signalait lui-même deux points non tranchés :
  l'interaction avec `exit_watch` (qui rend la main à la liste des sessions dès qu'une session
  sélectionnée sort) et la nécessité d'un plafond pour éviter qu'une commande qui échoue
  instantanément ne boucle indéfiniment. Ce document couvre ces deux questions et les décisions
  qui en découlent côté `pkg/config`/`pkg/session`/`pkg/gui` ; il ne touche à aucune décision des
  ADR 0001–0009.

## Contexte

`W` redémarre un groupe de sessions déjà sorties, mais seulement à la main (`pkg/gui/group.go`) ;
`R` fait de même pour la session sélectionnée (`pkg/gui/sessions_panel.go`). Il n'existe aucun
comportement automatique : un `npm run dev` qui casse dans une session cachée reste sorti jusqu'à
ce que quelqu'un le remarque et appuie sur une touche — exactement l'usage `make dev` que
`ROADMAP.md` visait pour faire de `lazyshell` un petit superviseur de développement.

## Décision 1 — Trois politiques, une valeur inconnue retombe sur `never`

`SessionSpec.Restart string` (`never` par défaut, `on-failure`, `always`) suit la doctrine de
liste blanche déjà posée par ce fichier : décrire ce qui existe, jamais l'apparence de
l'interface — même raisonnement que `GroupSpec`/`WatchSpec` (ADR 0009, Décision 5). Une valeur
inconnue *ne fait pas tomber la session* : contrairement à un `cwd` manquant ou un motif de veille
qui ne compile pas (une liste d'entrées indépendantes, où une mauvaise entrée ne coûte qu'elle-
même), `restart:` est un scalaire unique avec un défaut sûr — le même traitement que
`Config.Validate` réserve à une `language` inconnue. `resolveRestartPolicy` retombe donc sur
`RestartNever` et le signale, sans jamais abandonner la session qui le porte.

## Décision 2 — Recul exponentiel sans plafond de tentatives

Le délai avant un redémarrage automatique double à chaque tentative consécutive (1s, 2s, 4s...
plafonné à 60s par attente), sans limite sur le *nombre* de tentatives. **Cette décision inverse
délibérément ce que `ROADMAP.md` suggérait** (« un plafond de redémarrages est nécessaire, sinon
une commande qui échoue instantanément boucle ») : le choix retenu est que le ralentissement
exponentiel *est* la protection, pas un abandon après N échecs qui laisserait la session morte
sans qu'aucune action supplémentaire ne la relève — une commande qui échoue en boucle finit par ne
retenter que toutes les 60 secondes, jamais plus vite, et jamais moins souvent.

Le recul s'applique à *toute* tentative automatique, y compris une sortie à code 0 sous `always` —
pas seulement aux échecs consécutifs, malgré ce que le nom « compteur d'échecs consécutifs »
suggérerait naturellement. Une session `always` qui boucle proprement en sortie 0 (une commande
mal choisie, un script qui se termine tout de suite) a exactement le même besoin de
ralentissement qu'une session `on-failure` qui échoue en boucle.

## Décision 3 — Réinitialisation du compteur après une durée de stabilité, pas à chaque tentative

Le compteur de tentatives consécutives retombe à zéro une fois qu'un redémarrage tient
`RestartSuccessDuration` (10 secondes par défaut) — pas à chaque nouvelle tentative, ce qui ferait
paraître un cycle de plantage rapide comme une suite de démarrages neufs sans jamais ralentir pour
de bon. Ce minuteur de succès tourne dans le même goroutine `supervise` que celui qui arme le
redémarrage suivant, et s'arrête proprement dès que la session sort — les deux préoccupations
partagent la même durée de vie précisément parce qu'aucune ne doit survivre à la session qu'elle
surveille.

## Décision 4 — Suppression du vol de focus dans `exit_watch` via une méthode, pas un nouveau `Status`

`watchSelectedExit` (`pkg/gui/exit_watch.go`) désarme le pass-through et rend le focus à la liste
des sessions dès que `Status()` d'une session sélectionnée vaut `StatusExited`. Une option
envisagée était un troisième `Status` (« redémarrage en attente ») ; elle a été écartée après avoir
tracé chaque consommateur de `Status`/`StatusExited` : le format de sortie de `ctl list`
(`pkg/gui/control.go`), les trois portes `== StatusExited` de `Send`/`GroupSend`/`Restart`, et les
gardes de `restartGroup`/`restartSession`. Un nouveau `Status` aurait fallu le répercuter partout,
et surtout aurait forcé `drain()` à évaluer la politique de redémarrage de façon synchrone —
entremêlant une logique qui appartient à `Manager` dans le goroutine déjà dense de `Session.drain`.

`Session.WillAutoRestart() bool` — fonction pure de `status`, `exitCode`, `killedExplicitly` et
`opts.Restart` — résout ça sans toucher à `Status` : `watchSelectedExit` l'appelle et rend la main
sans marquer ni back-out quand elle répond vrai. Effet secondaire délibéré et bienvenu : `ctl`,
`restartGroup`, `restartSession` et `Manager.Restart` continuent de fonctionner sans aucun
changement, et « R »/« W » restent un raccourci immédiat qui court-circuite l'attente du recul —
sans code spécial pour ce cas, puisque `Status()` reste `StatusExited` pendant toute l'attente.

## Décision 5 — Un arrêt explicite l'emporte toujours sur la politique

`Session.Kill` pose `killedExplicitly = true` avant toute autre chose qu'elle fait — y compris son
retour anticipé quand la session est déjà sortie, en attente d'un redémarrage différé. Un arrêt
volontaire (« x » depuis le panneau, ou `Manager.Shutdown` à la fermeture de l'application, qui
appelle déjà `Kill` directement sur chaque session) doit toujours l'emporter sur `restart:`, quel
que soit ce que dit le code de sortie seul — le même principe que `systemctl stop` qui ne redéclenche
jamais `Restart=on-failure` sous systemd. Ce n'était pas demandé explicitement par `ROADMAP.md`,
mais son absence aurait rendu « x » sur une session qui boucle apparemment sans effet si un
redémarrage était sur le point de se déclencher.

## Décision 6 — Où vit l'état de supervision, et pourquoi la sécurité tient sans verrou dédié

`Manager.restarts map[string]*restartState` (compteur de tentatives, minuteur en attente) vit sous
le `m.mu` déjà existant, pas un nouveau verrou : chaque section critique qui le touche a aussi
besoin d'examiner `m.sessions` dans le même geste, et un second verrou n'aurait fait qu'ouvrir la
porte à des bugs d'ordonnancement pour aucun bénéfice.

La sécurité face à un redémarrage manuel, un arrêt ou une suppression concurrents ne repose *pas*
sur l'arrêt du minuteur au moment de l'armer (`cancelSupervision`, appelé par `Restart`/`Kill`/
`Remove`, n'est qu'une optimisation qui évite un réveil pour rien) : elle repose sur le fait que
`fireAutoRestart` — le callback du minuteur — revérifie `WillAutoRestart()` et l'identité
`m.sessions[id] == sess` juste avant de redémarrer, plutôt que de faire confiance à l'état posé au
moment de l'armement. `WillAutoRestart` et cette vérification d'identité sont toutes deux
idempotentes une fois `sess.Done()` fermé, ce qui referme toute fenêtre de course entre « armé » et
« déclenché » sans jeton d'annulation partagé.

## Conséquences

- Une clé de fichier de projet de plus (`sessions[].restart`), un marqueur de gouttière/détail de
  plus (`markers.restart`, `↻<compteur>`, colonne détail comme `commandExitIndicator` — largeur
  variable, pas dans les quatre colonnes fixes de la gouttière).
- `lazyshell ctl new` ne gagne pas de moyen de poser une politique de redémarrage — même exclusion
  que celle déjà actée pour `restart` dans `ctl` (voir `README.md` et `ROADMAP.md` : « volontairement
  absent, y compris pour les groupes »), étendue par analogie à la nouvelle politique.
- Les valeurs de recul et de durée de succès sont des constantes fixes dans cette itération, seulement
  exposées comme champs de `Manager` pour que les tests puissent les raccourcir — pas de réglage
  possible depuis le fichier de projet ou la config utilisateur.
- Pas de compte à rebours en direct dans l'interface, seulement le compteur de tentatives : `Manager`
  n'expose aujourd'hui pas l'heure du prochain déclenchement.
