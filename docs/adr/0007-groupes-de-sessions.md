# ADR 0007 — Groupes de sessions

- **Statut** : accepté.
- **Date** : 2026-08-09
- **Contexte** : le panneau des sessions était une liste **plate** depuis la phase 2, et son invariant
  « une ligne = une session » était consigné comme contrainte dure à deux endroits du code. Ce
  document le remplace. Il étend l'ADR 0006 (l'API de contrôle passe en lecture/écriture sur le
  groupe) sans en toucher les décisions, et ne parle ni de clavier de session, ni de pass-through,
  ni de rendu de la sortie — les ADR 0001 à 0005 sont intacts.

## Contexte

Une liste plate suffisait tant qu'on ouvrait trois ou quatre shells. Avec six à huit sessions
d'agents IA — ce que la phase 11 a précisément rendu confortable, marqueurs d'état et notifications
comprises — elle ne dit plus rien : on voit huit lignes, on ne voit pas lesquelles travaillent sur le
même chantier. Et l'on ne peut agir que session par session, alors que l'unité de travail réelle est
« ces quatre agents-là ».

Ce dont il manquait le vocabulaire, c'est donc : *nommer un sous-ensemble de sessions, et lui parler
comme à un tout.*

## Décision 1 — Un groupe est une propriété de la session, et il n'y en a qu'un

`Session.group` — une chaîne, sous le même mutex que `name`, vide pour « sans groupe ». Pas de type
`Group`, pas de registre dans le `Manager`, pas de liste d'appartenances.

**Un seul groupe par session**, et c'est un choix, pas une simplification provisoire. Plusieurs
étiquettes par session se rendraient mal en arbre — il faudrait élire une étiquette principale pour
décider sous quel en-tête dessiner la ligne, ce qui revient à un seul groupe avec des étapes en
plus — et rendraient ambiguë chaque action de groupe (`X` sur une session qui est à la fois dans
`api` et dans `urgent` tue quoi ?). L'ambiguïté est le vrai coût, pas la souplesse perdue.

Le champ vit sur la session plutôt que dans une `map[sessionID]string` côté GUI parce que deux
chemins n'ont pas de GUI sous la main : `autostart()` crée les sessions du projet avant que
l'interface existe, et `Handler.List()` de l'API de contrôle tourne **en place** sur une goroutine de
connexion. Une map côté GUI aurait exigé un second mutex là où `Session.mu` existe déjà et porte
exactement cette forme de concurrence pour `name`.

Un piège, épinglé par un test : `Manager.Restart` lit `old.opts` **sans verrou**, ce qui n'est
correct que parce que `opts` est immuable après construction. `SetGroup` n'y écrit donc jamais ;
`Restart` reporte la valeur vive explicitement (`opts.Group = old.Group()`), de sorte qu'un
regroupement fait à la main survive à une relance.

## Décision 2 — Le regroupement est un calcul de rendu ; `Manager.order` ne bouge pas

`Manager.order` reste l'ordre de **création**, celui dont `Restart` préserve la place et que
`Handler.List()` promet à l'API de contrôle. Le regroupement est recalculé à chaque tick par
`orderByGroup`, à partir de l'ensemble **visible**.

L'ordre : groupes déclarés par le fichier de projet dans l'ordre de déclaration ; puis groupes créés
à chaud, dans leur ordre de première apparition ; puis les sessions sans groupe. Stable à l'intérieur
d'un groupe. L'ordre de déclaration plutôt qu'alphabétique parce que l'ordre du fichier est ce que
son auteur a voulu dire ; trier détruirait cette information sans rien apporter.

Les en-têtes se déduisent du visible, jamais de la liste déclarée : un groupe dont toutes les
sessions sont filtrées n'affiche rien, plutôt qu'une case vide.

## Décision 3 — Un arbre à en-têtes non repliables, et `selectedIndex` désigne toujours une session

Le panneau devient un arbre. Les en-têtes sont affichés, **non sélectionnables et non repliables**.

L'invariant remplacé était explicite, et documenté comme contrainte dure dans `sessionsPanelContent`
et dans `clickSession` : `selectedIndex` == numéro de ligne == index dans la liste. Quatre choses en
dépendaient à la fois : `selectedSession`, le `FocusPoint` du rendu, la surbrillance de gocui (qui
peint la ligne du curseur) et le `opts.Y` de la souris.

Le modèle retenu introduit `panelRow{text, sess}` — `sess == nil` pour une ligne non sélectionnable —
et garde `selectedIndex` dans l'espace des **sessions**, en ordre d'affichage. La conversion
session → ligne (`rowLineForSessionIndex`) n'a lieu qu'à la frontière du rendu, et la conversion
inverse (`sessionIndexForRowLine`) uniquement pour le clic, seule entrée qui arrive sous forme de
numéro de ligne.

C'est cette asymétrie qui porte la correction : **un en-tête ne peut pas être surligné, parce que la
fonction de conversion n'a aucun moyen de renvoyer sa ligne.** Ce n'est pas une vérification qu'on
peut oublier d'écrire, c'est une propriété du type de retour. Aucune logique « sauter l'en-tête » n'a
donc été ajoutée à la navigation : l'en-tête n'existe pas dans l'espace où `j`/`k` travaillent.

L'autre modèle — `selectedIndex` devient un index de ligne, avec du saut d'en-tête dans
`selectionMoved` — a été écarté pour une raison de fond et non de goût : l'espace des lignes **n'est
pas stable**. Affecter un groupe, ou une session qui sort d'un filtre, peut glisser un en-tête sous
le curseur entre deux ticks, et il n'existe alors aucune réparation naturelle. L'espace des sessions
préserve la propriété « `selectedIndex` nomme toujours une session », dont le clamp de
`selectedSession` dépendait déjà.

Non repliables : le repli est de l'état à faire persister, à rendre dans la gouttière, et à
réconcilier avec le filtre (que veut dire « replié » pour un groupe qu'un filtre réduit à une
session ?). Pour un panneau de quarante colonnes où le nombre de groupes se compte sur une main, le
prix dépasse le gain. La décision est réversible : le modèle de lignes est précisément ce qui la
rendrait peu coûteuse à changer.

## Décision 4 — Le fichier de projet déclare des groupes, jamais leur apparence

`ProjectConfig` gagne `groups: [{name}]` et `SessionSpec` gagne `group:`. **Un nom, et rien
d'autre** — pas de couleur, pas de glyphe, pas de touche, pas même un libellé d'affichage distinct du
nom.

C'est la doctrine de la liste blanche de `ProjectConfig` redite telle quelle : un `lazyshell.yml`
vient d'un dépôt, éventuellement de quelqu'un d'autre, et dit *ce qui existe* — jamais à quoi
ressemble l'interface de l'utilisateur ni ce que font ses touches. Un champ `label` a été écrit puis
retiré avant d'être livré : le nom est déjà la chaîne affichée, et un second champ disant la même
chose est exactement la surface spéculative que cette liste blanche existe pour tenir dehors.

Déclarer un groupe est facultatif et ne sert qu'à fixer l'ordre : `group: api` sans bloc `groups:`
fonctionne, et se range après les groupes déclarés. La seule forme rejetée est un nom contenant un
saut de ligne, qui déchirerait la ligne d'en-tête en deux et désynchroniserait le modèle de lignes de
ce qui est à l'écran — même règle appliquée aux noms venant du socket.

**Rien n'a été ajouté à la config utilisateur.** Un glyphe ou une couleur d'en-tête configurables
auraient coûté la ligne du tableau de référence du README, le bloc d'exemple, le gabarit
`lazyshell config init`, la validation et deux doc-tests, pour une option que personne n'a demandée.
Le format d'en-tête est une décision de code ; les cinq nouvelles touches passent par la map
`keybindings:` existante, qui ne demande pas de ligne de référence.

## Décision 5 — L'API de contrôle passe en lecture/écriture sur le groupe

`group` sur `SessionInfo` et sur `ctl new`, filtre `--group` sur `ctl list`, et trois verbes :
`group` (affecter/retirer), `group-send`, `group-kill`. La répartition en place / `onGUI` de
l'ADR 0006 est appliquée telle quelle et pour les mêmes raisons :

- `group-send` **en place**, comme `send` : les écritures vont à des ptys qui portent leur mutex.
- `group` via **`onGUI`**, contrairement à `rename` qui n'écrit qu'un nom : un regroupement change
  l'ordre d'affichage, dont la sélection est un index, donc la re-sélection et le repeint doivent se
  faire ensemble sur la goroutine de gocui.
- `group-kill` **en place** pour les kills, `onGUI` pour le seul repeint : N kills à `KillTimeout`
  chacun feraient sauter la garde de 2 s — la leçon déjà consignée dans le commentaire de `Kill`.

`Handler.New` passe d'arguments positionnels à un `NewSpec`, exactement ce que le commentaire de
`session.Options` dit avoir voulu éviter en son temps.

Un groupe vide ou inconnu est une **erreur**, jamais un « 0 session » réussi : l'appelant ne pourrait
sinon pas distinguer une faute de frappe d'un kill effectif, et c'est l'API où cette différence
signifie « votre kill n'a rien fait et vous croyez le contraire ».

**Relancer un groupe n'est pas exposé.** Aucun verbe `restart` n'existe pour une session seule ;
n'en ajouter un que pour les groupes serait incohérent avec le jeu de verbes minimal de l'ADR 0006.
Cela reste la touche `W`.

## Décision 6 — Les touches de groupe sont des majuscules, faute d'un aller-retour `Ctrl` correct

`g` (affecter), `G` (filtrer), `A` (diffuser), `X` (tuer), `W` (relancer).

Le jeu voulu était `Ctrl-B` / `Ctrl-X` / `Ctrl-R` : « la version groupe d'une action est la forme
Ctrl de sa touche » est une règle qu'on retient. Il a fallu y renoncer sur un fait vérifié :
`keyLabel` produit `"Ctrl-B"`, alors que `gocui.Parse` découpe sur `+` et ne connaît que `"CtrlB"`.
Or `TestREADMEKeybindingsMatchDefaults` impose que le README documente exactement la sortie de
`keyLabel`, et `TestValidateConfigAcceptsEveryDocumentedRemap` fait passer ce même bloc dans
`ValidateConfig` — une action remappable à touche Ctrl casse donc le build immédiatement. Aucune
liaison portant un `Action` n'utilise Ctrl aujourd'hui, ce qui rend le défaut invisible.

**C'est une dette identifiée, laissée hors périmètre** : la corriger (normaliser le libellé avant
`gocui.Parse`, et épingler l'aller-retour par un test) est un chantier propre et indépendant, et le
mélanger à celui-ci aurait fait porter à une fonctionnalité le risque d'un correctif qui la dépasse.
Tant qu'elle n'est pas réglée, tout l'espace `Ctrl` est fermé aux actions remappables.

## Décision 7 — La diffusion de groupe réutilise `broadcastMarks`

`A` marque toutes les sessions du groupe, plutôt que d'introduire un mode « je diffuse à un groupe ».
Il reste ainsi une seule règle d'armement (`broadcastArmed`), un seul glyphe de gouttière, une seule
liste de diffusion, et le chemin des frappes (`dispatchKey`) ignore complètement l'existence des
groupes.

Conséquence assumée, et à première vue surprenante : les marques sont indexées par **id de session**,
donc une session qui quitte le groupe ensuite reste marquée. C'est correct — les marques sont ce que
l'utilisateur a armé, et en retirer une silencieusement à cause d'un regroupement sans rapport serait
pire qu'une marque visible dans la gouttière et effaçable d'une touche.

Le filtre, lui, est un champ à part (`groupFilter`) et non un préfixe `group:` dans le filtre
textuel : le préfixe rendrait ambiguë une session réellement nommée `group:x`, demanderait un
analyseur, et — la vraie raison — ne pourrait se poser qu'en ouvrant la popup, là où `G` le pose en
une frappe. Les deux se composent en ET, et `Échap` les efface tous les deux.

## Conséquences

- L'invariant « exactement une ligne par session », documenté comme contrainte dure dans
  `sessionsPanelContent` et `clickSession`, n'existe plus. Les deux commentaires ont été réécrits pour
  renvoyer au modèle de lignes.
- `selectNewlyCreatedSession` ne peut plus prendre le dernier index : la création ajoute en fin
  d'ordre manager, mais une nouvelle session d'un groupe placé tôt atterrit au milieu de l'affichage.
  Elle reçoit désormais la session créée et la retrouve par id.
- `Handler.New` change de signature, et `sessionsPanelContent` gagne une largeur (les en-têtes
  remplissent le panneau). Cette largeur voyage par un champ mis en cache pendant le layout : la lire
  depuis `view.InnerWidth()` sur la goroutine de rendu serait une course.
- Une session ne peut pas appartenir à deux groupes, et les en-têtes coûtent une ligne chacun.
- Les kills d'un groupe sont **concurrents**, et c'est une exigence de correction, pas une
  optimisation. Découvert en pilotant le vrai binaire, pas par les tests : en séquence, un groupe de
  **deux** dépassait déjà le délai client de 3 s de `pkg/control`, et `ctl group-kill` renvoyait un
  timeout de transport pour des kills qui avaient réussi — exactement la réponse que cette API ne
  doit jamais donner. `killSessions` est partagé avec la touche `X`, qui y gagne un spinner
  proportionnel au membre le plus lent plutôt qu'à leur somme.
- `CLAUDE.md` disait « six verbes » pour l'API de contrôle ; c'est à jour ici.
- Une dérive préexistante, constatée en écrivant le report de groupe à travers `Restart` et laissée
  telle quelle : `SetName` a le problème symétrique, une session renommée reprend `opts.Name` à la
  relance. Hors périmètre, signalée par un commentaire à l'endroit voulu plutôt que corrigée en
  silence.
