# ADR 0011 — Le pass-through devient l'état par défaut

- **Statut** : accepté.
- **Date** : 2026-08-12
- **Contexte** : étend l'**ADR 0004** et l'**ADR 0005**, sans les remplacer — exactement comme 0005
  le fait déjà pour 0004. L'automate de sortie du pass-through (une pression, la paire `Échap`
  `Échap` dans `escExitWindow`) reste inchangé à la lettre ; seule sa signification change : « sortir
  du pass-through » devient « entrer en mode verrouillé ».

## Contexte

Depuis l'ADR 0004, le panneau de sortie démarre en mode défilement, et le pass-through — les frappes
envoyées telles quelles au shell de la session — est une bascule volontaire : `i`/`Entrée` pour y
entrer, `Ctrl-O` (ou `Échap` `Échap`) pour en sortir. C'était le bon défaut tant que défiler,
chercher ou copier étaient les actions les plus fréquentes sur le panneau — ce n'est pas le cas :
taper dans une session, en particulier une session d'agent IA, est l'action la plus fréquente de
très loin. La cérémonie `i`/`Entrée` à chaque sélection de session est un coût récurrent payé pour
l'action la plus commune, au bénéfice de l'action la plus rare.

## Décision 1 — Le pass-through devient l'état par défaut

`passThroughActive` (`pkg/gui/gui.go`) démarre à `true` (`New`), et devient un drapeau global unique
que seul un geste explicite de verrouillage/déverrouillage modifie : changer la session sélectionnée
— `j`/`k`, un clic, la molette — ne le touche plus. Avant cet ADR, ce n'était pas un choix : le
pass-through n'existait que par vue courante et par armement explicite (`enterPassThrough`), donc la
question ne se posait pas. Maintenant qu'il persiste, il fallait trancher, et le choix est la
persistance pure : atterrir sur une autre session en étant verrouillé vous y dépose verrouillé,
y atterrir déverrouillé vous y dépose prêt à taper. C'est plus simple à retenir qu'un réarmement
automatique par session, et cohérent avec l'esprit d'un drapeau global plutôt que d'un état par
session.

Deux moments continuent de changer le drapeau à la place de l'utilisateur, parce que l'intention y
est sans ambiguïté et que ni l'un ni l'autre n'est concerné par la persistance ci-dessus (elle ne
porte que sur le changement de sélection) :

- **Démarrer ou relancer une session** (`n`/`N`/`M`/`c`/`R`) arme le pass-through sans condition,
  même si le panneau était verrouillé l'instant d'avant (`focusSelectedShell`, inchangé) : on ne crée
  pas une session pour la regarder.
- **Un shell qui se termine de lui-même** verrouille le panneau (`backOutOfExitedSession`, inchangé) :
  il n'y a plus personne à qui envoyer les touches.
- **Quitter l'onglet `terminal`** pour `perf`/`env` verrouille aussi (`setTab`, inchangé) et ne
  réarme pas au retour : `editOutput` teste `passThroughActive` avant l'onglet actif, donc taper sur
  un rapport statique doit rester impossible. Ce point reste hors du périmètre de la persistance de
  la décision 1, qui ne porte que sur la sélection de session, pas sur les onglets.

## Décision 2 — Le mode verrouillé garde toutes ses actions, sans combinaison de touches

Le mode aujourd'hui par défaut (`editDuringScroll`) ne change pas de contenu : copy-mode (`v`),
recherche (`/`), pagination (`PgUp`/`PgDn`/`Ctrl-U`/`Ctrl-D`), zoom (`z`), changement d'onglet
(`]`/`[`), `q`, `?` restent tous à une touche, une fois verrouillé — rien n'est retiré, rien n'est
réduit au simple défilement. Il aurait été possible d'aller plus loin et de forcer ces actions
derrière une combinaison façon `Ctrl-O` + lettre pour les déclencher sans quitter le pass-through :
choix explicitement écarté. Cela aurait réintroduit l'automate à délai que la décision 1 de l'ADR
0004 a justement supprimé — pour l'action de sortie elle-même, cette fois — pour un gain d'usage
marginal face au coût d'apprentissage et de latence. La touche préfixe et la paire `Échap` `Échap`
restent donc des sorties pures, « une pression, un effet », exactement comme les ADR 0004 et 0005 les
ont définies.

## Décision 3 — Couleur : renommage et correction d'une bavure

Le vert (`theme.ActiveBorderColor`) et le rouge (anciennement `theme.PassThroughBorderColor`)
échangent de sens : vert marque désormais le pass-through (déverrouillé, l'état normal), rouge marque
le verrouillage. Puisque rouge ne signifie plus « pass-through » mais l'inverse, son nom devait
changer pour ne pas mentir — `PassThroughBorderColor`/`pass_through_border_color` devient
`LockedBorderColor`/`locked_border_color`, sans alias de rétrocompatibilité (même pratique que le
changement de défaut `Ctrl-B`→`Ctrl-O` de l'ADR 0004 : un renommage franc plutôt qu'une clé morte à
maintenir).

Ce renommage a mis au jour une bavure de couleur restée invisible tant que le pass-through était
l'exception : `g.SelFrameColor` (gocui) est un unique attribut global, peint sur la vue actuellement
courante — pas par vue. `refreshChrome` le fixait uniquement d'après `passThroughActive`, sans
regarder quelle vue avait le focus. `cutControlToSessions` (`pkg/gui/mouse.go`) compensait en
désarmant le pass-through à chaque clic ou coup de molette sur le panneau de sessions, pour que la
bordure verte d'origine y reste correcte. Avec un drapeau désormais persistant qui ne doit plus se
désarmer au clic (décision 1), ce filet de sécurité disparaît, et sans lui la bordure « verrouillé »
(rouge) aurait peint le panneau de sessions chaque fois que le drapeau est à `false` — y compris en
pure navigation, un contresens.

Le vrai correctif : `borderColorFor`/`updateBorderColor` (`pkg/gui/input.go`) calculent la couleur à
partir de la vue courante *et* du drapeau — le rouge ne s'applique qu'à la vue de sortie elle-même,
jamais au panneau de sessions ni à une popup. `focusManager` (`pkg/gui/focus.go`), qui existait déjà
pour détecter un changement de focus que gocui ne signale pas lui-même, est ce qui déclenche ce calcul
sur chaque changement — pas seulement les événements qui appelaient `refreshChrome` avant cet ADR.

## Conséquences sur l'interface

- `cutControlToSessions` ne désarme plus le pass-through au clic ou à la molette (voir décision 3).
- `docs/repports/` et le `README`/`docs/README.fr.md`/`site/` sont recadrés : `Ctrl-O`/`Échap`
  `Échap` deviennent l'action notable (« verrouiller »), `i`/`Entrée` devient l'action de retour
  (pertinente seulement une fois verrouillé) — l'inverse du cadrage d'avant cet ADR.
- Le pied de panneau et la barre de statut gagnent un indicateur symétrique côté verrouillé
  (`status.locked_hint`) : le pass-through avait déjà le sien (`status.passthrough`/
  `status.passthrough_esc`), et le mode verrouillé, devenu l'état occasionnel, en avait besoin
  autant.

## Ce qui ne change pas

- `editDuringPassThrough`, `escExitWindow`, la résolution de `prefixKey`/`$LAZYSHELL_PREFIX`, et la
  garantie « un `Échap` transmis, jamais zéro, jamais deux » (ADR 0005) sont inchangés à la lettre.
- `backOutOfExitedSession` (`pkg/gui/exit_watch.go`) continue de désarmer le pass-through quand le
  process shell est mort — raison différente de celle qui faisait désarmer `cutControlToSessions`,
  et donc non concernée par la décision 1.
- `setTab` continue de désarmer en quittant l'onglet `terminal` et ne réarme pas au retour.
- `cmd/spike-pty` garde l'automate d'origine de la phase 1, artefact conservé pour mémoire.
- Les ADR 0004 et 0005 ne sont pas réécrites : elles restent l'enregistrement de ce qui avait été
  décidé à leur date.
