# Roadmap

Pistes d'évolution pour `lazyshell`, au-delà de ce qui est déjà construit. Ce fichier ne rejoue pas
l'historique des phases 0–13 (il est dans le git log, et ce qu'il disait de l'architecture est dans
`CLAUDE.md`) : il ne liste que ce qui reste devant.

**Statuts** : `idée` (identifié, rien de décidé) · `à concevoir` (retenu, design à écrire, souvent un
ADR) · `en cours` · `fait` · `abandonné`.

Une entrée passe à `à concevoir` quand on décide de la faire, et `fait` quand elle est dans `main`
avec sa doc (README + `docs/README.fr.md` + `site/`) — voir la politique de langue dans `CLAUDE.md`.

---

## 1. Persistance de la disposition entre deux lancements

**Statut : fait** — [ADR 0013](docs/adr/0013-persistance-de-la-disposition.md)

Le mode démon reste hors scope (les pty ne survivent pas), mais la *recette* de chaque session —
`nom + cwd + command + groupe` — survit à la sortie dans
`~/.config/lazyshell/state/<hash-du-cwd>.yml`, et le lancement suivant propose de la restaurer.

La restauration n'est offerte qu'en l'absence de `lazyshell.yml` (un fichier projet, déclaratif et
passé par le magasin de confiance, garde toujours la priorité sur un enregistrement implicite) ;
`restore_layout: ask` (défaut) affiche une popup, `always` restaure sans la montrer, `never` ne la
propose jamais — l'état est écrit à la sortie dans tous les cas. Aucun changement dans `pkg/session`
au-delà d'un accesseur pur (`Session.Command()`) ; `SessionSpec` reste intact, le fichier d'état
utilise son propre type plus étroit (pas d'`env`/`watch`/`restart`/`locked`, hors du périmètre que ce
point fixait).

## 2. `ctl wait` — attendre un état au lieu de le sonder

**Statut : idée**

Le verbe qui manque côté orchestration. Aujourd'hui un agent chef d'orchestre doit boucler sur
`ctl list` pour savoir qu'une session est passée `blocked`.

```sh
lazyshell ctl wait build --state done --timeout 300
lazyshell ctl wait --group agents --state blocked   # rend la main au premier qui bloque
```

- Reste en lecture seule / déclaratif : ne déplace pas le curseur de sécurité posé par
  l'ADR 0006, donc pas de nouvel ADR attendu.
- Contrainte : c'est la première requête *longue* du protocole ligne-JSON, qui est aujourd'hui
  strictement requête→réponse immédiate. Il faut décider comment le serveur tient la connexion
  ouverte sans bloquer la boucle GUI (cf. la répartition de goroutines de `pkg/gui/control.go`, qui
  est une règle de correction et non de style).
- `--timeout` obligatoire ou défaut ? Sortie non nulle sur timeout, cohérente avec le reste de `ctl`.

## 3. Intégration shell OSC 133 (bornes de commande)

**Statut : fait** — [ADR 0008](docs/adr/0008-integration-shell-osc-133.md)

Le panneau de sortie est un vrai émulateur ; comprendre les marques `OSC 133;A/B/C/D` (posées par
zsh/fish/bash via leur hook d'intégration shell standard) a débloqué d'un coup :

- saut à l'invite précédente / suivante dans le scrollback (`{` / `}`) ;
- copie de la sortie de la dernière commande en une touche (`Y`) ;
- **code de sortie** de la dernière commande dans la liste des sessions (`✗ <code>`, colonne nom/état,
  pour une session sans agent) — bien plus parlant que le marqueur d'activité ;
- notification « la commande a échoué » pour les sessions **non-agent**, que `notify` ne couvrait
  pas jusque-là.

La contrainte que cette entrée signalait — les bornes doivent survivre à la troncature du
scrollback — est résolue par un identifiant monotone ajouté au fork `charmbracelet/x/vt`
(`Scrollback.Evicted()`), détaillé dans l'ADR. Les bornes sont aussi suspendues tant qu'un écran
alterné (`vim`, `htop`) tient la session.

## 4. Watchers de motifs par session

**Statut : fait** — [ADR 0009](docs/adr/0009-watchers-de-motifs-par-session.md)

Généralisation de la notification agent aux sessions ordinaires : un serveur de dev qui casse dans
une session cachée ne se signalait jusque-là que par un `●`.

```yaml
sessions:
  - name: api
    watch:
      - pattern: "ERR!"
        notify: true
```

La touche `v` arme un motif à la volée sur la session sélectionnée, en plus de ce que
`lazyshell.yml` a déjà déclaré pour elle ; un motif vide désarme. Réutilise le canal de
notification existant (OSC 9 / 777 + `notify.fallback_command`). L'anti-rebond que cette entrée
signalait comme non tranché est résolu par un plafond de 3 secondes par motif armé (Décision 3 de
l'ADR) ; le tap sur la sortie brute est partagé avec la détection d'agent (Décision 1), et
`watch:` dans un fichier projet suit la doctrine de liste blanche de `ProjectConfig` comme
`groups:` (Décision 5).

## 5. `restart: on-failure` dans le fichier projet

**Statut : fait** — [ADR 0010](docs/adr/0010-redemarrage-automatique-des-sessions.md)

Une politique par session (`never` / `on-failure` / `always`, `never` par défaut), avec recul
exponentiel et compteur de tentatives affiché dans la liste (`↻<compteur>`), fait de `lazyshell` un
petit superviseur de dev — exactement l'usage `make dev` / `npm run dev` du fichier projet. `W`/`R`
continuent de redémarrer une session à la main, immédiatement, court-circuitant l'attente d'un
recul en cours.

L'interaction avec `exit_watch` que cette entrée signalait est résolue par `Session.WillAutoRestart`
(Décision 4 de l'ADR) : une session pour laquelle un redémarrage automatique va se déclencher ne
rend jamais le focus à la liste des sessions. Le plafond que cette entrée jugeait nécessaire est
délibérément *un recul sans plafond de tentatives* plutôt qu'un abandon après N échecs (Décision 2)
— le ralentissement exponentiel est la protection retenue.

## 6. Recherche globale sur toutes les sessions

**Statut : idée**

`/` cherche dans la session courante. Un raccourci séparé qui grep les scrollbacks de *toutes* les
sessions et liste `session → ligne`, sélectionnable pour y sauter, répond à « lequel des huit agents
a parlé de ce fichier ? ». Toute la donnée est déjà en mémoire.

- Réutilise `pkg/gui/search.go` pour le rendu de correspondance ; l'ajout est l'agrégation et le
  panneau de résultats.
- Choix de touche contraint : les touches `Ctrl` restent inutilisables pour une action remappable
  tant que la sortie de `keyLabel` ne fait pas l'aller-retour par `gocui.Parse` (décision 6 de
  l'ADR 0007).

## 7. Réordonner les sessions

**Statut : idée**

`Manager.order` est figé sur l'ordre de création. Déplacer une session vers le haut / le bas (dans
son groupe) rend la liste utilisable au-delà de dix sessions.

- Touche à choisir sous la même contrainte que ci-dessus.
- Invariant à respecter (ADR 0007) : ça touche `Manager.order`, jamais l'indexation des lignes —
  `gui.selectedIndex` indexe des **sessions** en ordre d'affichage.

## 8. Jeton optionnel sur la socket de contrôle

**Statut : idée**

`control.token: <chaîne>`, injecté dans les sessions comme `$LAZYSHELL_CONTROL_TOKEN`, vérifié à
l'ouverture de connexion.

- Ne change pas le défaut : `control.enabled` reste `false`, et sans jeton configuré le
  comportement est exactement l'actuel.
- Répond au seul point noir documenté de l'ADR 0006 (« pas de jeton, les permissions `0600` sont
  tout le contrôle d'accès ») pour une machine partagée ou avec des process tiers.
- Impose une mise à jour de l'ADR 0006 (complément, pas remplacement).

## 9. `locked:` dans le fichier projet — verrouillage par session

**Statut : fait** — [ADR 0012](docs/adr/0012-verrouillage-par-session.md)

Une session déclarée avec une `command:` démarre verrouillée (`locked: false` pour surcharger) : on
la consulte sans qu'une frappe parasite puisse tuer la commande en cours. L'état verrouillé /
pass-through devient mémorisé par session, ce qui amende la décision 1 de l'ADR 0011 (persistance
pure d'un drapeau global) pour les sessions dont l'état a été tranché ; tout le reste garde cette
persistance.

---

## Hors scope (rappel)

Ces points sont tranchés, ils ne sont pas des idées en attente :

- **Windows** : hors scope, pas de pty Unix.
- **Mode détaché / démon** (« les agents tournent capot fermé ») : hors scope sauf demande réelle.
  L'idée 1 ci-dessus n'est *pas* ça — elle ne fait survivre que la description des sessions.
- **`restart` dans `ctl`** : volontairement absent, y compris pour les groupes (voir README) ; ça
  reste la touche `W`.
