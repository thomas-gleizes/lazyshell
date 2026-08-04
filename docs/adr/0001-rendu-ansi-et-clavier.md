# ADR 0001 — Stratégie de rendu ANSI et traduction clavier

- **Statut** : accepté pour les phases 1 à 5, à revisiter en phase 6.
- **Date** : 2026-08-04
- **Contexte** : phase 1 de `ROADMAP.md` (spike pty), point go/no-go du projet.

## Contexte

`lazygit` et `lazydocker` n'ont aucun précédent utile ici : leurs flux sont sortants uniquement
(logs Docker), et les commandes interactives passent par `Suspend()`/`Resume()` du terminal entier.
`lazyshell` doit au contraire faire cohabiter gocui et un pty interactif dans le même terminal, en
permanence. Trois questions devaient être tranchées avant d'investir dans l'UI.

## Décision 1 — Rendu ANSI : (a) + `OutputTrue` + filtrage (c)

**Cette décision a été révisée après exécution du spike.** La version initiale retenait l'option (a)
seule (`io.Copy` du pty vers la vue, sans filtrage). L'essai réel l'a invalidée : avec un prompt zsh
thémé, l'écran est illisible **avant même** de lancer une application plein écran.

Mesures faites sur gocui `v0.3.1-0.20260331125330`, en écrivant des séquences dans une vue headless
et en relisant `View.Buffer()` :

| Séquence | `OutputNormal` | `Output256` | `OutputTrue` |
|---|---|---|---|
| `ESC[31m` (SGR 8 couleurs) | consommée | consommée | consommée |
| `ESC[38;5;2m` (256 couleurs) | **affichée en clair** | consommée | consommée |
| `ESC[38;2;r;g;bm` (truecolor) | **affichée en clair** | **affichée en clair** | consommée |
| `ESC[K` (efface la ligne) | traitée | traitée | traitée |
| `ESC]0;titre BEL` (OSC) | consommée | consommée | consommée |
| `ESC[A`, `ESC[H`, `ESC[2J` | **affichées en clair** | idem | idem |
| `ESC[?25l`, `ESC[?2004h`, `ESC[?1049h` | **affichées en clair** | idem | idem |
| `ESC=` (keypad) | **affichée en clair** | idem | idem |

Deux conséquences, toutes deux corrigées :

1. **`OutputTrue` est obligatoire, pas cosmétique.** En `OutputNormal`, tout prompt thémé (p10k,
   starship, oh-my-zsh) émet du SGR 256 couleurs et s'affiche en `[38;5;2;m` littéral. C'est la
   majorité du bruit observé sur le spike.
2. **Le filtrage (c) est nécessaire**, et non « du masquage de symptôme » comme écrit initialement :
   gocui n'ignore pas les séquences qu'il ne comprend pas, il en **affiche le corps** (l'octet ESC
   est mangé, le reste est imprimé). Sans filtre, un simple prompt produit `[?2004h`, `[?25l`, `[A`,
   `[J` à l'écran. `pkg/ansi` ne laisse donc passer que SGR, `ESC[K` et OSC.

Ce qui reste vrai : les applications plein écran (`vim`, `htop`, `less`) **ne sont pas supportées**
avant la phase 6. Le filtrage les rend lisibles-mais-absurdes (les redessins en place deviennent des
lignes empilées) au lieu d'illisibles. La dette reste entière, elle est seulement contenue.

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

## Décision 4 — Le tampon d'affichage doit être borné dès maintenant

Découvert en lançant `htop` dans le spike : après en être sorti, l'application ne répondait plus au
clavier. Ce n'était pas un problème de clavier mais de rendu.

Une `gocui.View` conserve **tout** ce qui y a été écrit, et le coût d'un redraw croît linéairement
avec ce tampon. Mesuré sur une vue 80×24 :

| Lignes dans le tampon | Durée d'un redraw |
|---|---|
| 12 000 | 11 ms |
| 36 000 | 25 ms |
| 72 000 | 49 ms |

Le tick de rendu est à 30 ms. Passé ~40 000 lignes, un redraw coûte plus cher que l'intervalle entre
deux : la boucle principale sature et les touches ne sont plus traitées. Une application qui
redessine en place en produit des dizaines de milliers en quelques minutes.

Le spike plafonne donc le tampon (5 000 lignes, ramené à 4 000 à chaque dépassement). C'est une
mesure provisoire : la phase 2 la remplace par le ring buffer prévu. Le point important est que
**la borne du scrollback n'est pas une optimisation mémoire, c'est une condition de réactivité de
l'UI** — la roadmap la présentait comme une simple protection contre la fuite mémoire.

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
- `pkg/ansi` : filtrage séquence par séquence, y compris un cas réel de prompt zsh, et un test qui
  coupe le flux à **chaque** position possible (une séquence peut être scindée entre deux lectures
  du pty).
- `cmd/spike-pty` : plafonnement du tampon d'affichage.
- `cmd/spike-pty/pty_test.go` : un vrai `/bin/sh` derrière un pty, piloté par les octets produits
  par `pkg/keys` — `echo` exécuté, `stty size` conforme à la taille du pty, redimensionnement
  propagé au shell en cours d'exécution, `Ctrl-C` qui interrompt le job au premier plan.

Manuelle, dans un vrai terminal (`go run ./cmd/spike-pty`) : la partie que seul un humain peut
observer, à savoir le rendu.

## Conséquences

- Le go/no-go technique est **go** : rien dans gocui n'empêche de piloter un pty interactif. Le
  clavier, le resize et le cycle de vie fonctionnent ; c'est le rendu, et lui seul, qui est limité.
- **La phase 6 est plus proche du chemin critique que ne le supposait la roadmap.** Le rendu
  filtré est acceptable pour lire une sortie de commande, pas pour vivre dans un shell thémé.
  L'arbitrage « MVP filtré vs. émulateur tout de suite » est à trancher avant la phase 3.
- La phase 4 réutilise `pkg/keys` tel quel dans `pkg/gui/input.go`.
- Le coût de la phase 6 est confirmé comme réel mais isolé : il porte sur le rendu, pas sur le
  clavier ni sur le cycle de vie des process.
- La souris reste hors périmètre, désormais pour une raison technique documentée et non par simple
  priorisation.
