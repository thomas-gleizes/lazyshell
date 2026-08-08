# ADR 0005 — `Esc` `Esc` comme seconde sortie du pass-through

- **Statut** : accepté.
- **Date** : 2026-08-08
- **Contexte** : complète l'**ADR 0004**, qu'il ne remplace pas. La décision 1 (une pression, sortie
  immédiate) et la décision 2 (`Ctrl-O` par défaut) restent en vigueur telles quelles.

## Contexte

L'ADR 0004 a réparé la sortie du pass-through, mais il en reste un défaut qu'il ne traitait pas :
`Ctrl-O` ne se devine pas. C'est une touche qu'on apprend en lisant le pied de panneau ou le
README ; personne ne la trouve en tâtonnant. La touche que tout le monde essaie d'abord pour sortir
de quelque chose, c'est `Échap`.

La faire *devenir* la touche d'échappement a été envisagée puis écartée, pour la raison même qui a
fait tomber `Ctrl-B` en décision 2 de l'ADR 0004 : la décision 1 supprime toute séquence « envoyer
littéralement », donc la touche d'échappement n'est plus tapable dans une session. Or `Échap` est
la touche la plus utilisée *dans* les sessions :

- `vim` : sans elle, on ne quitte pas le mode insertion ;
- Claude Code : c'est l'interruption de l'agent, dans un outil dont l'hébergement de sessions
  d'agents est la raison d'être depuis la phase 11 ;
- `less`, `htop`, tout dialogue ncurses.

S'y ajoute une raison technique propre au terminal : `Échap` n'y est pas une touche mais un
préfixe. Les flèches, les touches F et `Alt+lettre` arrivent toutes sous la forme `ESC` + suite, et
tcell ne distingue un `Échap` isolé d'un début de séquence que par un délai. Un `Échap` seul est
donc livré en retard, et une frappe rapide `Échap` puis autre chose peut être relue comme
`Alt+touche`. La touche la plus évidente aurait été la plus capricieuse.

## Décision — le double appui, pas le préfixe

Deux `Échap` consécutifs, séparés de moins de `escExitWindow` (400 ms), sortent du pass-through.
`Ctrl-O` (ou ce que dit `prefix_key`) reste la sortie principale, inchangée.

Ce qui rend la chose acceptable, et qui la distingue de l'automate à préfixe supprimé par l'ADR
0004 :

1. **Le premier `Échap` part dans la session immédiatement**, comme n'importe quelle autre touche.
   Rien n'est retenu, rien n'attend un délai. `Échap` continue donc de fonctionner dans `vim` et
   dans une session d'agent, et le problème de latence décrit plus haut ne se pose pas — lazyshell
   ne rajoute aucune attente à celle que tcell fait déjà.
2. **Aucune touche n'est avalée à tort.** L'automate de la phase 1 avalait la frappe suivant un
   préfixe pressé par mégarde ; ici, seul le second `Échap` d'une paire est retenu. Toute autre
   touche entre les deux casse la paire (`Échap`, `j`, `Échap` est un utilisateur de `vim` qui se
   déplace, pas quelqu'un qui demande à sortir).
3. **L'état expire.** C'était le défaut 3 de l'ADR 0004 : `escExitWindow` est ce qui empêche un
   `Échap` tapé maintenant et un autre cinq minutes plus tard de faire une sortie.

Coût assumé, et il faut le dire tel quel : **sortir par cette voie envoie un `Échap` dans la
session** (le premier de la paire), jamais zéro et jamais deux. Dans `vim` c'est bénin ; dans une
session Claude Code, cela interrompt l'agent. Et le réflexe du double `Échap` de `vim`, chez qui
l'a, fera sortir du pass-through. `Ctrl-O` reste la sortie sans effet de bord pour ces cas-là.

La fenêtre de 400 ms est un compromis assumé, pas une valeur mesurée : assez large pour un double
appui volontaire, assez courte pour que deux `Échap` sans rapport ne se rejoignent pas. Elle n'est
pas configurable pour l'instant — une clé de config de plus pour un réglage que personne n'a encore
demandé à changer.

## Conséquences sur l'interface

- La paire est testée **après** le `prefix_key`. Un utilisateur qui règle `prefix_key: Esc` obtient
  la simple pression qu'il a demandée, et la paire ne s'applique pas.
- Dans ce cas précis, le pied de panneau et la barre de statut n'annoncent pas `Esc Esc` — ce
  serait enseigner un geste qui n'est pas le sien. D'où les deux messages `status.passthrough` et
  `status.passthrough_esc` dans `pkg/i18n`.
- `gui.lastEscAt` est remis à zéro à l'entrée comme à la sortie du pass-through : un `Échap` d'un
  pass-through précédent ne doit pas s'apparier avec le premier du suivant.

## Ce qui ne change pas

- L'ADR 0004 n'est pas réécrit ni contredit. `Ctrl-O` reste la sortie par défaut et la seule qui
  n'envoie rien à la session.
- Le clic sur le panneau des sessions (`pkg/gui/mouse.go`) sort toujours du pass-through.
- `cmd/spike-pty` garde l'automate d'origine de la phase 1, artefact conservé pour mémoire.
