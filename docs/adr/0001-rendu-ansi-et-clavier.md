# ADR 0001 — Stratégie de rendu ANSI et traduction clavier

- **Statut** : accepté. Décision 1 révisée deux fois après essais réels (voir son historique).
- **Date** : 2026-08-04
- **Contexte** : phase 1 de `ROADMAP.md` (spike pty), point go/no-go du projet.

## Contexte

`lazygit` et `lazydocker` n'ont aucun précédent utile ici : leurs flux sont sortants uniquement
(logs Docker), et les commandes interactives passent par `Suspend()`/`Resume()` du terminal entier.
`lazyshell` doit au contraire faire cohabiter gocui et un pty interactif dans le même terminal, en
permanence. Trois questions devaient être tranchées avant d'investir dans l'UI.

## Décision 1 — Émulateur de terminal, dès maintenant

**Révisée deux fois, à chaque fois par l'essai réel.** La version initiale retenait l'option (a) de
la roadmap (`io.Copy` du pty vers la vue). La deuxième ajoutait `OutputTrue` et un filtrage des
séquences non rendues (option (c)). Les deux sont abandonnées : on adopte l'option (b), un
véritable émulateur de terminal (`github.com/charmbracelet/x/vt`), **avant** la phase 3 et non en
phase 6.

### Ce que les mesures ont établi

D'abord, le mode de sortie de gocui n'est pas cosmétique. Séquences écrites dans une vue headless,
relues via `View.Buffer()` :

| Séquence | `OutputNormal` | `Output256` | `OutputTrue` |
|---|---|---|---|
| `ESC[31m` (SGR 8 couleurs) | consommée | consommée | consommée |
| `ESC[38;5;2m` (256 couleurs) | **affichée en clair** | consommée | consommée |
| `ESC[38;2;r;g;bm` (truecolor) | **affichée en clair** | **affichée en clair** | consommée |
| `ESC[K` | traitée | traitée | traitée |
| `ESC]0;titre BEL` (OSC) | consommée | consommée | consommée |
| `ESC[A`, `ESC[H`, `ESC[2J` | **affichées en clair** | idem | idem |
| `ESC[?25l`, `ESC[?2004h`, `ESC[?1049h` | **affichées en clair** | idem | idem |

Point clé : gocui n'ignore pas les séquences qu'il ne comprend pas, **il en affiche le corps**
(l'octet ESC est mangé, le reste est imprimé). D'où `[?2004h` et `[A` visibles à l'écran.

`OutputTrue` + filtrage supprimait ce bruit, mais laissait un défaut rédhibitoire : **le contenu
lui-même était faux**. Un prompt thémé se redessine sur place. Mesuré au seul démarrage d'un zsh
avec thème : **5 `ESC[A`, 4 `ESC[J`, 5 `ESC[K`, 16 `CR`** avant que l'utilisateur ne tape quoi que
ce soit. Le prompt est dessiné quatre fois, chaque version devant écraser la précédente. Un modèle
qui ne sait qu'*ajouter* du texte empile les quatre. « Remonter d'une ligne et réécrire » est
intraduisible sans grille de cellules.

### La décision

Le pty alimente un émulateur (`pkg/screen`), et la vue affiche l'écran que celui-ci calcule
(`Render()` produit l'écran complet avec les SGR, que gocui sait rendre en `OutputTrue`).

Conséquences, toutes vérifiées par des tests :

- Les redessins en place écrasent correctement : un prompt, pas quatre.
- **Le coût d'un redraw devient borné par la géométrie** (lignes × colonnes) au lieu de croître avec
  le volume de sortie. Cela règle du même coup le gel de l'UI décrit en décision 4.
- Le scrollback est fourni par l'émulateur (10 000 lignes par défaut), ce qui recouvre le ring
  buffer prévu en phase 2.
- L'émulateur **répond aux requêtes de capacités** du terminal (`Read()` renvoie ces réponses, à
  réinjecter dans le pty). Sans cela, le shell attend une réponse qui ne vient pas et les octets
  parasites finissent affichés.
- `IsAltScreen()` distingue « sortie de shell » de « vim a la main », information dont l'UI aura
  besoin en phase 4.
- `vim`, `htop` et `less` deviennent possibles — ce qui était l'objet de la phase 6.

`pkg/ansi` (le filtre) est supprimé : l'émulateur le remplace intégralement.

### Ce que ça coûte

`charmbracelet/x/vt` est un module **non versionné** (pseudo-version, API non stabilisée). C'est le
risque assumé du choix ; l'alternative `hinshun/vt10x` est plus simple mais non maintenue depuis
2022 et sans scrollback ni gestion des réponses. `pkg/screen` existe précisément pour que ce choix
reste remplaçable derrière une interface réduite (Write, Read, Render, Resize).

Reste ouvert pour la phase 3 : `gocui.View` reçoit une chaîne rendue à chaque frame, ce qui
fonctionne mais n'est pas un rendu cellule par cellule. Si le coût s'avère trop élevé avec plusieurs
panneaux, la question « descendre vers tcell pour le panneau output » se reposera.

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

Le plafonnement du tampon a d'abord été fait à la main (5 000 lignes). Il est devenu inutile avec
l'émulateur : la vue affiche un écran de taille fixe, donc le coût d'un redraw ne dépend plus du
volume de sortie. Le point important reste : **la borne du scrollback n'est pas une optimisation
mémoire, c'est une condition de réactivité de l'UI** — la roadmap la présentait comme une simple
protection contre la fuite mémoire.

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
3. **Le même `Ctrl-<lettre>` arrive sous plusieurs formes.** Selon le terminal et le protocole
   clavier qu'il utilise, `Ctrl-B` parvient soit comme `KeyCtrlB` sans modificateur, soit comme la
   rune `'b'` avec `ModCtrl` — et sous cette seconde forme il est **indistinguable d'un `b` tapé
   normalement**, ce qui a rendu le préfixe d'échappement inopérant. `keys.Normalize` replie toutes
   ces formes sur `KeyCtrlX` ; toute comparaison de touche doit se faire sur la forme normalisée.
   Corollaire : ne jamais exiger `mod == ModNone` pour la touche qui est la seule sortie de
   l'application.

## Vérification

Automatisée, sans terminal — c'est ce qui rend la phase 1 rejouable en CI :

- `pkg/keys` : table de traduction exhaustive, y compris les 23 `Ctrl-<lettre>`.
- `pkg/screen` : redessin en place qui écrase, effacement d'écran, couleurs préservées, taille du
  rendu bornée quel que soit le volume écrit, détection de l'alternate screen.
- `cmd/spike-pty/pty_test.go` : un vrai `/bin/sh` derrière un pty, piloté par les octets produits
  par `pkg/keys` — `echo` exécuté, `stty size` conforme à la taille du pty, redimensionnement
  propagé au shell en cours d'exécution, `Ctrl-C` qui interrompt le job au premier plan.

Manuelle, dans un vrai terminal (`go run ./cmd/spike-pty`) : la partie que seul un humain peut
observer, à savoir le rendu.

## Conséquences

- Le go/no-go technique est **go** : rien dans gocui n'empêche de piloter un pty interactif. Le
  clavier, le resize et le cycle de vie fonctionnent ; c'est le rendu, et lui seul, qui est limité.
- **La phase 6 a été avancée avant la phase 3** : le rendu filtré était acceptable pour lire une
  sortie de commande, pas pour vivre dans un shell thémé. Construire l'UI sur un modèle
  d'affichage voué à être remplacé revenait à la construire deux fois.
- La phase 2 est allégée : le scrollback vient de l'émulateur.
- La phase 4 réutilise `pkg/keys` tel quel dans `pkg/gui/input.go`.
- Le coût de la phase 6 est confirmé comme réel mais isolé : il porte sur le rendu, pas sur le
  clavier ni sur le cycle de vie des process.
- La souris reste hors périmètre, désormais pour une raison technique documentée et non par simple
  priorisation.
