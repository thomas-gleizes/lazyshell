---
name: lazyshell-agents
description: >
  À utiliser quand l'utilisateur demande comment piloter lazyshell depuis un
  agent IA (Claude Code, Codex, opencode) : câbler les hooks de statut
  (working/blocked/done), utiliser `lazyshell ctl` pour lister/lire/créer/
  piloter des sessions ou des groupes de sessions, comprendre `$LAZYSHELL_SOCK`
  / `$LAZYSHELL_CONTROL_SOCK` / `$LAZYSHELL_SESSION_ID`, ou évaluer les
  implications de sécurité d'activer `control.enabled`. Concerne les
  fonctionnalités "AI agent sessions" et "Agent control API" de lazyshell.
version: 1.0.0
---

# Fonctionnalités agents IA de lazyshell

lazyshell est un multiplexeur de sessions shell (type tmux/lazygit) qui a deux
canaux distincts pour interagir avec des agents IA lancés dans ses sessions.
Ne pas les confondre : ils ont des buts, une sécurité et une portée opposés.

| | Canal hooks | Canal control API |
|---|---|---|
| Sens | agent → lazyshell (déclaratif) | lazyshell ← agent (pilotage) |
| Socket | une par session, `$LAZYSHELL_SOCK` | une par process lazyshell, `$LAZYSHELL_CONTROL_SOCK` |
| Activation | toujours ouverte | `control.enabled: true` (faux par défaut) |
| Ce que ça permet | changer l'état affiché (`idle`/`working`/`blocked`/`done`) | lister, lire, créer, écrire, tuer, renommer, grouper des sessions |
| Échec | `lazyshell hook` sort toujours en 0 | `lazyshell ctl` sort non-nul en cas d'échec |

## 1. Détection d'état sans configuration

Une session dont le process au premier plan est un agent connu (`claude`,
`codex`, `opencode`) affiche automatiquement un marqueur d'état dans la
gouttière, déduit de l'écran visible et du titre du terminal via les
manifestes intégrés (`pkg/agent/manifests`). Aucune config requise. Pour
surcharger un manifeste ou en ajouter un : déposer un
`<process-name>.yml` dans `~/.config/lazyshell/agents/` (même nom = override,
nom différent = ajout).

Cette détection est une heuristique — dès qu'une session reçoit un seul
événement du canal hooks (section suivante), l'heuristique s'arrête
définitivement pour cette session : les hooks deviennent autoritaires.

## 2. Canal hooks — l'agent déclare son propre état

Chaque session a son socket Unix, exposé au process qui tourne dedans via
`$LAZYSHELL_SOCK` (avec `$LAZYSHELL_SESSION_ID`). `lazyshell hook <state>`
écrit dessus, avec `<state>` ∈ `idle`, `working`, `blocked`, `done`.

Ne pas taper ça à la main : le câbler dans le mécanisme de hooks natif de
l'agent. Pour obtenir la config prête à coller :

```sh
lazyshell init --agents
```

- **Claude Code** — bloc `hooks` dans `settings.json` :
  `UserPromptSubmit` → `lazyshell hook working`,
  `Notification` → `lazyshell hook blocked`,
  `Stop` → `lazyshell hook done`.
- **Codex** — une ligne `notify` dans `config.toml`. Codex n'a qu'un seul
  événement (`agent-turn-complete`), donc il ne peut rapporter que `done`.
- **opencode** — pas encore câblé (son signal le plus riche est un
  abonnement SSE, pas un push, donc une intégration de forme différente).

Ce canal ne fait jamais l'inverse : lazyshell n'appelle jamais l'agent au
travers de ce socket, il ne fait qu'écouter, et un événement ne peut rien
faire d'autre que positionner cet unique état. Il reste ouvert par défaut
précisément parce qu'il ne peut déplacer qu'un marqueur dans une liste — voir
`docs/adr/0006-api-de-controle-par-les-agents.md` pour la comparaison
complète avec le canal ci-dessous.

## 3. Canal control API — un agent pilote lazyshell

`control.enabled: true` (config utilisateur, faux par défaut) ouvre un
second socket, un par process lazyshell (pas un par session), exposé comme
`$LAZYSHELL_CONTROL_SOCK` dans l'environnement de chaque session — son
absence quand la fonctionnalité est désactivée est le signal, il n'y a pas
de flag séparé à vérifier. `lazyshell ctl` le pilote :

```sh
lazyshell ctl list                                # id, nom, statut, état agent, groupe
lazyshell ctl list --group agents                 # filtré par groupe
lazyshell ctl read session-2 --tail 40             # texte brut, sans codes d'échappement
lazyshell ctl new --name build --cwd ./api --command 'make test' --group agents
lazyshell ctl send build 'echo bonjour' --enter    # comme si tapé au clavier
lazyshell ctl kill build
lazyshell ctl rename build tests
```

Groupes (voir ADR 0007) — orchestrer plusieurs sessions comme une unité :

```sh
lazyshell ctl group build agents                  # ajoute une session à un groupe
lazyshell ctl ungroup build                       # l'en retire
lazyshell ctl group-send agents 'git pull' --enter
lazyshell ctl group-kill agents
```

Points à connaître avant d'utiliser ou de recommander ce canal :

- Une session se nomme par son id (`session-2`, la valeur de
  `$LAZYSHELL_SESSION_ID`) ou par son nom exact. `--json` donne la réponse
  brute au lieu du rendu humain.
- Les deux verbes de fan-out (`group-send`, `group-kill`) échouent sur un
  groupe vide ou inexistant — jamais un "0 session" silencieux qui ferait
  croire à une réussite. `group-send` saute les sessions déjà terminées.
- Pas de `restart` de groupe : il n'y a pas de `restart` pour une session
  seule non plus (`W` reste une touche de l'interface), donc en ajouter un
  seulement pour les groupes serait incohérent.
- `ctl new` ne vole ni la sélection ni le clavier (contrairement à la touche
  `n`) — un agent en tâche de fond qui crée une session worker ne doit pas
  arracher le curseur à ce que l'utilisateur est en train de taper.
- `lazyshell hook` sort toujours en 0 (ne doit jamais casser le tour d'un
  agent) ; `lazyshell ctl` sort non-nul en cas d'échec (un appelant qui
  demande une session doit savoir s'il ne l'a pas obtenue).

### Implication de sécurité — à dire avant d'activer

Il n'y a **ni jeton ni permission par session** : les droits `0600` sur le
fichier socket sont le seul contrôle d'accès. Activer `control.enabled`
signifie donc que **tout process tournant sous le compte de l'utilisateur**
peut créer des sessions, y taper des commandes et en lire la sortie — pas
seulement les agents lancés dans lazyshell. `ctl read` renvoie le
scrollback verbatim, secrets compris — le masquage de l'onglet env n'a pas
d'équivalent ici, un identifiant affiché à l'écran étant indiscernable de
tout autre texte.

C'est pourquoi c'est désactivé par défaut, et pourquoi c'est un socket et un
protocole séparés du canal hooks plutôt qu'une extension de celui-ci.
Toujours signaler ce compromis à l'utilisateur avant de suggérer
`control.enabled: true` dans une config. Détails complets et alternatives
pesées dans `docs/adr/0006-api-de-controle-par-les-agents.md`.

## 4. Notifications et suivi de tour

- Un passage à `blocked` ou `done` déclenche une notification desktop (OSC 9
  + OSC 777 vers le terminal hôte par défaut, ou la commande
  `notify.fallback_command` avec le texte sur stdin).
- `B` saute directement à la prochaine session `blocked`, en boucle.
- Une session `working` affiche la durée du tour en cours
  (`⏱ 1m32s`). `agent_stats_command` (au plus une fois toutes les 5s, pour
  la session sélectionnée) affiche sa première ligne de sortie à côté —
  pensé pour un lookup coût/tokens, pas pour tourner par session à chaque
  tick.

## Références

- `README.md`, section "AI agent sessions" (§403–528) : source de vérité,
  toujours vérifier que le comportement décrit ici la reflète encore.
- `docs/adr/0006-api-de-controle-par-les-agents.md` : pourquoi deux canaux
  séparés, alternatives écartées.
- `docs/adr/0007-groupes-de-sessions.md` : sémantique des groupes
  (`group`/`group-send`/`group-kill`), invariant sur `Manager.order`.
- `pkg/agent/manifests` : format des manifestes de détection.
- `pkg/control` : implémentation des neuf verbes `ctl`.
