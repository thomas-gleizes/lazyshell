# ADR 0004 — Sortie du pass-through : une touche, un effet

- **Statut** : accepté.
- **Date** : 2026-08-08
- **Contexte** : révise la **décision 3 de l'ADR 0001** (« Préfixe d'échappement : `Ctrl-B` ») et sa
  convention de doublement. Déclenché par un usage réel : sortir du mode interactif échouait
  régulièrement, en particulier dans une session Claude Code.

## Contexte

L'ADR 0001 avait retenu un préfixe à la tmux, avec l'automate correspondant, porté tel quel du
spike de phase 1 (`cmd/spike-pty`) vers `pkg/gui/input.go` :

- `Ctrl-B` seul **arme** le préfixe ;
- la touche suivante confirme la sortie, et est **avalée** ;
- `Ctrl-B` `Ctrl-B` envoie un `Ctrl-B` littéral au shell et **reste** en pass-through.

Trois défauts, invisibles tant qu'on ne s'en sert pas quotidiennement :

1. **La première pression ne produit aucun retour.** Ni le pied de panneau (`pkg/gui/footer.go`, qui
   affichait toujours « sortir ») ni la barre de statut (`pkg/gui/gui.go`) ne connaissaient l'état
   armé. L'utilisateur presse, rien ne bouge.
2. **Le réflexe qui en découle est exactement celui qui ne sort pas.** Presser à nouveau est le cas
   « littéral » : on reste en pass-through, et un octet de contrôle part dans la session. Dans une
   session Claude Code, `Ctrl-B` est *lié* (« passer cette commande bash en arrière-plan ») : la
   session réagit visiblement, ce qui confirme à l'utilisateur qu'il est toujours dans le shell et
   l'invite à recommencer. La boucle est stable.
3. **L'état armé n'expirait jamais.** Un préfixe pressé par mégarde faisait perdre la frappe
   suivante, à un moment arbitraire.

La documentation, elle, décrivait « le préfixe seul quitte » — ce que le code ne faisait pas. C'est
l'écart entre les deux qui a été corrigé, pas seulement la touche.

## Décision 1 — Une pression, sortie immédiate

`editDuringPassThrough` n'a plus d'état : la touche d'échappement sort du pass-through sur-le-champ,
aucune touche ne confirme, aucune touche n'est avalée. Le champ `prefixPending` disparaît.

Ce qu'on perd, explicitement : il n'existe plus de séquence signifiant « envoie cette touche
littéralement ». La touche d'échappement n'est donc plus tapable dans une session. C'est le prix
assumé de la simplicité, et c'est précisément pourquoi elle reste configurable (`prefix_key`,
`$LAZYSHELL_PREFIX`) : qui a besoin de la touche dans son shell la déplace ailleurs plutôt que de
perdre la sortie.

## Décision 2 — La touche par défaut devient `Ctrl-O`

`Ctrl-B` est abandonné. Le raisonnement de l'ADR 0001 tenait sur les shells, mais lazyshell a depuis
la phase 11 une raison d'être qui n'existait pas alors : héberger des sessions d'agents IA. `Ctrl-B`
est une touche que ces sessions-là utilisent — c'est le raccourci « arrière-plan » de Claude Code.
Une touche qui signifie quelque chose des deux côtés à la fois est un mauvais échappement, quel que
soit le reste de ses qualités.

Candidats écartés :

- **`Ctrl-]`**, l'échappement classique façon telnet : demande AltGr en AZERTY, et sert de « saut
  vers le tag » dans vim, qui tourne dans les sessions.
- **`Ctrl-Space`** : reste écarté pour la raison de l'ADR 0001 (tous les terminaux ne l'émettent pas
  distinctement), plus une raison propre au code — tcell l'encode en NUL, donc `Key` vaut `0`, ce
  qui est aussi la valeur portée par **toute** touche imprimable.
- **`Ctrl-A`**, **`Ctrl-S`**, **`Ctrl-Q`**, **`Ctrl-\`** : début de ligne readline, contrôle de flux
  XON/XOFF, `SIGQUIT`.
- **Touches F** : sans collision applicative (`htop` s'arrête à F10), mais loin des mains et
  interceptées par certaines configurations de terminal.

`Ctrl-O` est retenu : accessible sur tout clavier, sans collision avec Claude Code, `vim` ou `htop`,
et mnémonique (« Out »). Son usage shell — `operate-and-get-next` en bash, `accept-line-and-down-history`
en zsh — est marginal, et reste accessible en déplaçant `prefix_key`.

## Conséquence sur la comparaison des touches

`editDuringPassThrough` teste désormais `key == gui.prefixKey && ch == 0`. Le `ch == 0` n'est pas
défensif par principe : une touche de contrôle arrive sans rune, tandis qu'un caractère imprimable
arrive avec `Key` à `0` et la rune renseignée. Sans ce test, une touche d'échappement valant `0`
ferait sortir du pass-through à la première lettre tapée. `ValidateConfig` refuse déjà un
`prefix_key` qui n'est pas une touche de contrôle, mais `$LAZYSHELL_PREFIX` ne passe pas par elle.

## Ce qui ne change pas

- Le clic sur le panneau des sessions continue de sortir du pass-through (`pkg/gui/mouse.go`), et
  reste la sortie qui ne dépend d'aucune touche.
- `cmd/spike-pty` garde l'automate d'origine : c'est un artefact de la phase 1, conservé pour
  mémoire, pas un morceau du binaire livré.
- L'ADR 0001 n'est pas réécrit. Sa décision 3 est **remplacée par celle-ci**, comme le veut la
  convention : un ADR est un enregistrement de ce qui a été décidé à sa date.
