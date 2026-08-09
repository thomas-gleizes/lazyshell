# ADR 0008 — Intégration shell OSC 133

- **Statut** : accepté.
- **Date** : 2026-08-09
- **Contexte** : `ROADMAP.md` (« Intégration shell OSC 133 (bornes de commande) ») demandait de
  résoudre, avant d'écrire du code, le point qu'il signalait lui-même comme non tranché : comment des
  repères posés dans le scrollback survivent à sa troncature. Ce document couvre uniquement cette
  question et les décisions qui en découlent côté `pkg/screen`/`pkg/gui` ; il ne touche à aucune
  décision des ADR 0001–0007.

## Contexte

Les shells qui posent l'intégration standard (zsh, fish, bash) émettent `OSC 133;A` au début d'un
prompt, `;B` au début de la saisie, `;C` au début de la sortie de la commande, et `;D[;code]` à sa
fin. Les capter permet de sauter d'un prompt à l'autre dans le scrollback, de copier la sortie de la
dernière commande d'une touche, d'afficher son code de sortie dans la liste des sessions, et de
notifier un échec pour une session qui n'est pas un agent IA détecté (les sessions agent ont déjà
leurs propres notifications `blocked`/`done`).

`pkg/screen` enveloppe un fork local de `charmbracelet/x/vt` (`third_party/charmbracelet/x/vt/`,
`replace` dans `go.mod` — voir aussi le README du fork). Son `Scrollback` est une tranche `[]uv.Line`
toute simple : `Push` évince la ligne la plus ancienne dès que `maxLines` est atteint
(`scrollback_size`, 10000 par défaut), `SetMaxLines` évince en bloc en cas de réduction — et ni l'une
ni l'autre ne préviennent l'appelant. La convention déjà utilisée partout dans `pkg/screen`
(`Find`, `RenderAt`, `TextRange`) — un « index absolu » `i`, converti en position d'affichage par
`offset = ScrollbackLen() - i` — change donc de sens en silence à chaque éviction : un repère stocké
tel quel désignerait une ligne différente au bout d'assez de sortie, sans qu'aucun signal ne le
révèle. C'est précisément le problème qu'un enregistrement en session `htop` ou `vim` laissé ouvert
plusieurs jours ferait ressortir, à retardement et sans message d'erreur.

## Décision 1 — Un identifiant monotone via `Scrollback.Evicted()`, pas une réécriture en anneau

Le fork gagne un compteur `evicted int` sur `Scrollback`, incrémenté du nombre de lignes évincées à
chaque appel de `Push`, `SetMaxLines` ou `Clear`, exposé par `Evicted() int`. Un repère est alors posé
comme **identifiant monotone** — `Evicted() + position` au moment où l'événement OSC arrive — et lu
en le retraduisant en position courante par `id - Evicted()` ; un résultat négatif signifie que la
ligne visée est sortie du scrollback, et se traduit par `ok=false` côté `pkg/screen`, jamais par un
index au hasard.

L'alternative considérée — réécrire `Scrollback` en tampon circulaire réellement indexé par
identifiant monotone, éliminant `slices.Delete` — a été écartée : elle toucherait tous les
consommateurs existants de `Scrollback` (`Line`, `Lines`, `CellAt`, et tout `pkg/screen` qui suppose
un index 0 = ligne la plus ancienne), pour un gain (éviction en `O(1)` plutôt qu'en `O(n)`) que les
tailles de scrollback de lazyshell ne réclament pas. Le correctif retenu tient dans un seul fichier,
une quinzaine de lignes, documenté dans le README du fork.

## Décision 2 — Les repères vivent dans `pkg/screen`, pas dans le fork

Le fork ne fait que compter les évictions. Tout ce qui concerne OSC 133 — la machine à états
prompt/commande/sortie, l'analyse des sous-champs `A`/`B`/`C`/`D;<code>`, la porte d'écran alterné
(Décision 3) — reste côté lazyshell, dans un nouveau fichier `pkg/screen/osc133.go`. C'est la
discipline déjà posée par l'ADR 0001 (« garder `vt` remplaçable ») : un futur changement de fork n'a
qu'à réintroduire `Evicted()`, jamais à réimplémenter l'intégration shell.

Les accesseurs publics de `Screen` (`PromptMarks`, `LastCommandOutputRange`, `LastCommandExit`)
suivent la forme déjà en vigueur pour tout le reste de `Screen` : verrouillés par `s.mu`, ne
renvoient que des valeurs simples, ne laissent fuir aucun type de `vt`.

## Décision 3 — La porte d'écran alterné : un drapeau, pas une reconstruction

Un `vim` ou un `htop` en écran alterné peut écrire n'importe quoi, y compris des séquences qui
ressembleraient à `B`/`C`/`D` sans venir du shell. Plutôt que de tenter de reconstruire ce qui
appartient réellement au shell, le passage à l'écran alterné (`AltScreen(true)`) abandonne tout cycle
en cours ; le retour (`AltScreen(false)`) arme `awaitingPromptAfterAlt`, qui ignore `B`/`C`/`D`
jusqu'au prochain `A` effectivement observé. Ce n'est qu'un prolongement du principe déjà posé par
l'ADR 0002, Décision 5 pour l'écran alterné : signaler l'état, jamais changer de comportement de sa
propre initiative.

## Décision 4 — Le code de sortie s'affiche dans `detail`, pas dans la gouttière fixe

`sessionMarkers` (`pkg/gui/sessions_panel.go`) construit une gouttière à largeur fixe précisément
parce qu'une entrée à largeur variable décalerait toutes les lignes de la liste — un code de sortie
(`✗ 1` contre `✗ 127`) est par nature variable. Le gabarit déjà en place pour ce cas est celui de la
durée de tour d'un agent (`"⏱ …  "`, préfixée à `detail` dans `sessionLine`) : le nouvel indicateur le
suit à l'identique, plutôt que d'élargir la gouttière.

## Conséquences

- Deuxième correctif dans le fork `charmbracelet/x/vt`, documenté dans son propre README —
  contrairement au premier (course de données), celui-ci n'a pas vocation à disparaître le jour où le
  fork amont comble l'écart : rien n'indique qu'un tel compteur serve à quelqu'un d'autre que
  lazyshell.
- Une clé de configuration de plus (`markers.command_failed`), une clé i18n de plus
  (`notify.command_failed`), deux actions remappables de plus (`jump_prev_prompt`,
  `jump_next_prompt`) plus `copy_last_output` — chacune nécessitant la double inscription déjà en
  vigueur pour toute action qui doit rester joignable pendant le défilement (liaison normale +
  branche dans `editDuringScroll`).
- `Scrollback.Evicted()` devient une API publique du fork que toute fusion future avec une nouvelle
  version amont devra soit retrouver dans l'amont, soit continuer de porter indépendamment du
  correctif de course.
