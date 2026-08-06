# Analyse — intégration des agents de code IA dans lazyshell

Périmètre : **ne pas** embarquer de modèle ni de prompt dans lazyshell. Traiter les CLI d'agents
(`claude`, `codex`, `opencode`, `amp`, `cursor-agent`…) comme des **processus de session de première
classe** : savoir dans quel état ils sont, le montrer dans la liste, prévenir quand ils attendent, et
afficher les quelques chiffres utiles. lazyshell reste un multiplexeur ; l'IA est un *type de
locataire* de session, pas une fonctionnalité de lazyshell.

Document d'analyse — les décisions qui en sortent sont reportées dans `ROADMAP.md` (phase 11).

---

## 1. État de l'art

### herdr — la référence directe

`herdr` (Rust / ratatui, v0.7.0, juin 2026) est un multiplexeur de terminal dont l'argument unique
est la **conscience de l'état des agents**. C'est exactement l'outil visé par la demande initiale.

- Chaque panneau est classé dans un des **quatre états** : `idle`, `working`, `blocked`, `done`,
  affichés dans une barre latérale (vert / jaune / rouge / bleu).
- **Trois mécanismes de détection**, par ordre d'autorité :
  1. **hooks de cycle de vie** installés dans l'agent — « authoritative when installed and actively
     reporting for the running pane » ;
  2. **manifestes de détection** (TOML) évalués contre un *snapshot du bas de l'écran*, plus le titre
     du terminal et les séquences de progression — `~/.config/herdr/agent-detection/<agent>.toml`,
     avec **mise à jour distante depuis herdr.dev** (désactivable par `manifest_check = false`) ;
  3. **process au premier plan** du pty, qui sert à choisir quel manifeste appliquer.
- `blocked` est **volontairement strict** : uniquement sur une UI d'approbation / question /
  permission connue et visible. Le reste retombe sur `working`.
- **Notification desktop** à la fin d'un tour, et notification distincte quand l'agent se bloque en
  cours de route.
- **API socket Unix locale** : un agent « chef » peut créer des panneaux, lire leur sortie et
  réagir sans intervention humaine.
- Divers : détach/réattach, replay d'historique de panneau, mode SSH distant, `herdr agent attach
  <nom>`, orientation souris, préfixe `ctrl+b` par défaut. 20+ agents supportés.

### Le reste du paysage

| Outil | Nature | Ce qu'il apporte |
|---|---|---|
| `ccmux` | multiplexeur TUI | splits + bordure orange quand Claude Code tourne — détection binaire, pas d'états |
| `CodeAgentSwarm` | app Electron | notifications desktop, titres dynamiques, task board, suivi des fichiers modifiés |
| `claude-squad` | TUI + tmux + worktrees git | une branche isolée par agent — problème adjacent, pas le même |
| `ccusage` | CLI de stats | coût / tokens lus depuis les transcripts JSONL de Claude Code |

Le constat partagé par tous ces projets : **tmux ne distingue pas « il y a eu de l'activité » de
« il t'attend »**. Le drapeau d'activité d'une fenêtre en arrière-plan est vrai dans les deux cas.
C'est le manque, et c'est précisément le manque que la phase 7 de lazyshell allait combler à moitié
(marqueur d'activité) sans le combler tout à fait.

---

## 2. Ce que lazyshell a déjà, et qui rend ça peu coûteux

L'ADR 0001 a acheté quelque chose qui n'était pas payé pour ça : **une grille de cellules par
session**. C'est exactement l'entrée dont un manifeste de détection a besoin, et c'est ce que herdr
appelle son « live bottom-buffer screen snapshot ».

| Brique existante | Ce qu'elle donne à l'intégration agents |
|---|---|
| `pkg/screen` (émulateur `vt`) | snapshot d'écran interrogeable, sans rejouer le flux d'octets |
| `screen.Title()` (OSC) | beaucoup d'agents y écrivent leur état ou la commande en cours |
| `screen.BellPending()` | Claude Code sonne sur demande de permission — signal déjà capté et déjà à verrou |
| goroutine de drain par session | point d'observation par octet, **hors** de la boucle de rendu |
| gouttière de marqueurs (`pkg/gui/sessions_panel.go`) | `!` et `#` existent ; un marqueur d'état d'agent s'y ajoute, il ne se crée pas |
| `SessionSpec{Command, Env}` (phase 6) | déclarer `command: claude` **et injecter une variable d'environnement par session** |
| phase 7 (relance, saut par index, barre d'aide) | l'ergonomie multi-sessions que l'intégration agents suppose |

Un point mérite d'être souligné, parce qu'il est un **avantage structurel sur herdr** : comme
lazyshell est le parent qui lance le shell, il peut injecter `$LAZYSHELL_SESSION_ID` et
`$LAZYSHELL_SOCK` dans l'environnement de *chaque* session. La corrélation « ce hook vient de cette
session » est alors triviale, là où herdr doit la déduire du process au premier plan.

Manques assumés, hérités de décisions déjà actées : pas de daemon (le détach est hors périmètre,
phase 2), donc pas de « mes agents tournent pendant que lazyshell est fermé ». Ça ne bloque rien
ici, mais ça ferme d'avance la porte au cas d'usage « je ferme le laptop » que herdr vend.

---

## 3. Les sources de signal, par ordre de fiabilité

### (a) Hooks natifs de l'agent — autoritatif

C'est le seul canal qui donne l'état **exact** au lieu de le deviner. Chaque agent a le sien :

- **Claude Code** — hooks configurés dans `settings.json`, un JSON sur stdin contenant `session_id`,
  `transcript_path`, `cwd`, `hook_event_name`. Les événements utiles ici :
  `UserPromptSubmit` → `working` · `Notification` (permission demandée, ou attente prolongée) →
  **`blocked`** · `Stop` → `done` · `SessionStart` / `SessionEnd` → apparition / disparition.
  C'est la correspondance la plus propre des trois : les quatre états de herdr sont directement
  observables, sans regex.
- **Codex** — clé racine `notify = ["/bin/bash", "…/notify.sh"]` dans `~/.codex/config.toml`, un
  seul événement `agent-turn-complete` (`{type, thread-id, turn-id, cwd, input-messages,
  last-assistant-message}`). Donne `done`, pas `blocked`. Un système de hooks plus large est en
  cours côté Codex mais n'est pas un socle stable sur lequel s'appuyer aujourd'hui.
- **opencode** — un serveur HTTP avec un flux **SSE** (`/event`, `/global/event`) :
  `session.created/updated/deleted`, `session.status` (busy / idle), `message.updated`,
  `permission.updated`. Le canal le plus riche des trois, mais c'est un abonnement réseau à tenir,
  pas un hook qui pousse — un adaptateur différent en nature.

Conséquence de conception : lazyshell n'appelle jamais l'agent ; il **écoute**. Une sous-commande
`lazyshell hook <event>` que l'utilisateur branche dans la config de son agent, qui écrit sur le
socket désigné par `$LAZYSHELL_SOCK`. Trois adaptateurs, un seul protocole interne.

### (b) Snapshot d'écran + manifeste — le fallback qui marche sans rien installer

Regex sur les N dernières lignes de la grille et sur le titre OSC. C'est ce que fait herdr en repli,
et c'est ce qui permet de rendre l'outil utile **avant** que l'utilisateur ait configuré quoi que ce
soit — condition sine qua non pour que la fonctionnalité soit adoptée.

Trois règles non négociables :

1. **Déclaratif, pas de code Go par agent.** Un fichier YAML par agent (le repo en fournit pour
   claude / codex / opencode, l'utilisateur peut en ajouter dans `~/.config/lazyshell/agents/`).
   Sinon chaque changement d'UI d'un agent devient une release de lazyshell.
2. **Jamais de mise à jour distante.** herdr télécharge ses manifestes depuis son propre domaine :
   c'est du contenu distant qui pilote l'interprétation de ta sortie de terminal. Refusé. Manifestes
   locaux uniquement, versionnés dans le dépôt.
3. **`blocked` strict.** Seulement des motifs d'UI d'approbation explicitement listés. Dans le doute,
   `working`. Un faux « il t'attend » détruit la confiance dans le marqueur bien plus vite qu'un
   `blocked` manqué.

### (c) Process au premier plan — dit *qui*, pas *quoi*

`tcgetpgrp(ptmx)` puis `/proc/<pid>/comm` (Linux) / `sysctl` (macOS). Sert à choisir le manifeste et
à étiqueter la session, jamais à déduire un état. Un fichier par OS, dans l'esprit du reste du
projet — et le seul morceau non portable ajouté par cette intégration.

### (d) Fichiers d'état de l'agent — pour les stats, jamais pour le temps réel

Vérifié sur cette machine :

- Claude Code : `~/.claude/projects/<cwd-slug>/<session-id>.jsonl`, avec un `message.usage` complet
  par réponse (`input_tokens`, `output_tokens`, `cache_read_input_tokens`,
  `cache_creation_input_tokens`, `service_tier`).
- Codex : `~/.codex/*.sqlite` (`logs_2`, `state_5`, `goals_1`, `memories_1`) — base locale, schéma
  interne, numéroté (donc migré sans préavis).

Ces formats ne sont **pas des contrats**. Ils sont exploitables pour « combien ce tour a coûté »,
pas pour piloter un affichage temps réel. Et `ccusage` fait déjà ce travail : la bonne intégration
est une **commande externe configurable dont on affiche la ligne de sortie** (modèle `statusLine` de
Claude Code), pas une réimplémentation de la comptabilité des tokens dans lazyshell.

---

## 4. Ce qu'on retient et ce qu'on écarte

**Retenu** — ce qui répond au manque réel (« activité ≠ il t'attend ») :

- état d'agent à quatre valeurs dans la gouttière de la liste, avec couleur ;
- **notification** sur `blocked` et sur `done` — via **OSC 9 / OSC 777 vers le terminal hôte**, pas
  via un appel à `notify-send`. Même raison que le choix d'OSC 52 en phase 9 : ça traverse SSH, et
  ça ne dépend pas d'un binaire installé. Commande externe configurable en repli.
- **saut vers la prochaine session bloquée** en une touche. C'est l'ergonomie qui justifie tout le
  reste : à 6 agents ouverts, on ne navigue plus, on répond à celui qui appelle.
- stats de tour légères : durée du tour en cours, et tokens/coût best-effort via adaptateur externe.

**Écarté**, explicitement :

- embarquer un modèle, un prompt, un chat — hors périmètre posé d'entrée ;
- **API socket de contrôle** à la herdr (un agent qui crée des panneaux et lit la sortie des
  autres) : c'est une surface d'exécution offerte à un processus non fiable, pour un usage de
  niche. Le socket de lazyshell reste **entrant et déclaratif** — un agent y déclare *son* état, il
  ne pilote rien. À reverser dans « ce qui reste ouvert », pas dans la phase ;
- worktrees git par agent, task board, historique de conversation cherchable
  (`claude-squad`, `CodeAgentSwarm`) : c'est un autre produit, qui se construirait *au-dessus* de
  lazyshell et non dedans.

---

## 5. Risques

| Risque | Portée | Traitement |
|---|---|---|
| Couplage à des formats non contractuels (JSONL, sqlite, UI des agents) | **le risque principal** | tout derrière `pkg/agent` ; `pkg/session` et `pkg/gui` ne connaissent qu'une interface `State() AgentState`. Un agent qui casse dégrade un marqueur, il ne casse pas le multiplexeur |
| Faux positifs `blocked` | confiance de l'utilisateur | motifs stricts et listés ; défaut `working` |
| Coût de rendu | régression mesurée | l'évaluation des manifestes tourne dans la **goroutine de drain**, sur changement d'écran, throttlée (≤ 1 fois / 500 ms / session) — jamais dans la boucle de rendu. `TestIdleSessionDoesNotRepaint` doit rester à **0 repeint** au repos |
| Surface de confiance de l'env injectée | sécurité | `$LAZYSHELL_SOCK` est lisible par tout process de la session. Le protocole ne transporte que des *faits* (id de session, état, libellé) ; aucun verbe. Socket en `0600` dans `$XDG_RUNTIME_DIR` |
| Manifestes distants | sécurité | interdits. Local uniquement |
| Dépendance à `notify-send` / plateforme | portabilité | OSC 9 d'abord, commande configurable ensuite |

---

## 6. Recommandation

**Pertinent, et à faire — mais après les phases 6 et 7, pas avant.** L'intégration réutilise le
`SessionSpec` + `Env` de la phase 6 et, côté UI, la gouttière de marqueurs, le saut par index et la
barre d'aide contextuelle de la phase 7. Faite avant, elle les réinventerait en double ; faite
après, elle n'ajoute qu'un producteur de signal et un consommateur d'affichage.

Découpage en trois incréments livrables séparément (détail dans `ROADMAP.md`, phase 11) :

- **11a** — `pkg/agent`, détection du process au premier plan, manifestes YAML, état dans la
  gouttière. **Zéro configuration utilisateur** : c'est ce qui rend la phase testable tout de suite.
- **11b** — canal autoritatif : `lazyshell hook`, socket par session, env injectée, adaptateurs
  Claude Code / Codex / opencode.
- **11c** — notifications OSC 9, saut vers la prochaine session bloquée, stats de tour.

Position dans le séquencement : **v1.1**, après la v1.0 (phase 10, faite). C'est la première
fonctionnalité qui ne consiste pas à rattraper tmux mais à faire quelque chose que tmux ne fait pas
— et, à date, le seul concurrent sur ce terrain est herdr.
