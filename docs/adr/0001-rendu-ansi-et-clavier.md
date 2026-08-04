# ADR 0001 — Stratégie de rendu ANSI et traduction clavier

- **Statut** : accepté pour les phases 1 à 5, à revisiter en phase 6.
- **Date** : 2026-08-04
- **Contexte** : phase 1 de `ROADMAP.md` (spike pty), point go/no-go du projet.

## Contexte

`lazygit` et `lazydocker` n'ont aucun précédent utile ici : leurs flux sont sortants uniquement
(logs Docker), et les commandes interactives passent par `Suspend()`/`Resume()` du terminal entier.
`lazyshell` doit au contraire faire cohabiter gocui et un pty interactif dans le même terminal, en
permanence. Trois questions devaient être tranchées avant d'investir dans l'UI.

## Décision 1 — Rendu ANSI : MVP « line-oriented » assumé

On retient l'option (a) de la roadmap : `io.Copy` du pty vers la `gocui.View`, sans émulateur de
terminal.

Conséquence assumée et à documenter dans le README : les commandes qui écrivent en flux
(`ls`, `git log`, un prompt coloré, la sortie d'un build) fonctionnent ; les applications plein
écran (`vim`, `htop`, `less`) **ne sont pas supportées** avant la phase 6. gocui interprète un
sous-ensemble de SGR (couleurs, gras) mais ignore le positionnement de curseur (`ESC[H`, `ESC[2J`)
et l'alternate screen.

On n'ajoute **pas** de filtrage des séquences non supportées (option (c)) : cela masquerait le
symptôme sans rapprocher de l'objectif, et le spike a justement besoin de montrer la corruption
telle qu'elle est.

Contrainte de conception qui découle de ce choix et qui doit être respectée dès la phase 2 : une
session expose **« l'état à afficher »**, pas « un flux d'octets ». Le jour où un émulateur
(`vt10x`, `charmbracelet/x/vt`) alimentera une grille de cellules, seul l'intérieur de la session
change, pas le code d'affichage.

## Décision 2 — gocui garde le terminal en permanence

Pas de `Suspend()`/`Resume()` pendant le pass-through : suspendre gocui reviendrait à rendre le
terminal à une seule session et à perdre le multiplexage, qui est la raison d'être du projet.

## Décision 3 — Préfixe d'échappement : `Ctrl-B`

En pass-through, `Tab`, `Esc`, `q` et les flèches doivent partir au shell. Il faut donc un préfixe
à la tmux pour revenir à la navigation.

`Ctrl-B` est retenu :

- `Ctrl-A` est écarté : c'est « début de ligne » dans readline, utilisé en permanence dans un shell.
- `Ctrl-Space` est écarté : il transporte NUL, que tous les terminaux n'émettent pas distinctement.
- `Ctrl-B` (« caractère précédent » dans readline) est couvert par la flèche gauche dans l'usage
  courant. C'est aussi le préfixe par défaut de tmux, donc un réflexe déjà acquis.

Convention : `Ctrl-B` deux fois envoie un `Ctrl-B` littéral au shell. Le préfixe sera remappable via
la configuration en phase 5.

## Traduction clavier

gocui livre des événements **déjà décodés** (`Key` + rune), pas des octets bruts : il faut les
ré-encoder. La table vit dans `pkg/keys` — hors de `pkg/gui` pour être testable sans terminal — et
non dans le spike, qui est jetable.

Deux pièges découverts pendant le spike, tous deux invisibles à la lecture de la documentation :

1. **`Ctrl-<x>` ne vaut pas l'octet de contrôle.** gocui reprend les constantes de tcell, où
   `KeyCtrlA` vaut **65** (le code ASCII de `A`), pas 1. Tout l'intervalle `[64, 95]` est une
   combinaison de contrôle, l'octet à envoyer étant `key & 0x1f`. Les touches déjà représentées par
   leur octet (`Enter` = 13, `Tab` = 9, `Esc` = 27, `Backspace` = 8) sont, elles, sous 32.
2. **Shift-flèches et boutons de souris partagent les mêmes valeurs.** gocui n'a pas de constantes
   propres et réutilise les `F62`/`F63` inutilisés de tcell pour `KeyShiftArrowUp`/`KeyShiftArrowDown`,
   qui sont exactement `MouseRight`/`MouseLeft`. Les deux sont donc indistinguables. C'est
   acceptable tant que `g.Mouse = false` ; **activer la souris (point ouvert n°3 de la roadmap)
   impose de renoncer aux Shift-flèches**, ou de descendre sous gocui pour lire l'événement tcell.
   Un test (`TestShiftArrowsCollideWithMouseButtons`) échouera si un bump de gocui change cela.

## Vérification

Automatisée, sans terminal — c'est ce qui rend la phase 1 rejouable en CI :

- `pkg/keys` : table de traduction exhaustive, y compris les 23 `Ctrl-<lettre>`.
- `cmd/spike-pty/pty_test.go` : un vrai `/bin/sh` derrière un pty, piloté par les octets produits
  par `pkg/keys` — `echo` exécuté, `stty size` conforme à la taille du pty, redimensionnement
  propagé au shell en cours d'exécution, `Ctrl-C` qui interrompt le job au premier plan.

Manuelle, dans un vrai terminal (`go run ./cmd/spike-pty`) : la partie que seul un humain peut
observer, à savoir le rendu.

## Conséquences

- Le go/no-go technique est **go** : rien dans gocui n'empêche de piloter un pty interactif.
- La phase 4 réutilise `pkg/keys` tel quel dans `pkg/gui/input.go`.
- Le coût de la phase 6 est confirmé comme réel mais isolé : il porte sur le rendu, pas sur le
  clavier ni sur le cycle de vie des process.
- La souris reste hors périmètre, désormais pour une raison technique documentée et non par simple
  priorisation.
