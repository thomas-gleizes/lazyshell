#!/usr/bin/env bash
#
# Calcule le prochain tag semver à partir des commits accumulés depuis le
# dernier tag, selon la convention Conventional Commits que suit l'historique
# du dépôt :
#
#   feat!: … / BREAKING CHANGE dans le corps  → majeure
#   feat: …                                   → mineure
#   fix: … / perf: … / refactor: …            → corrective
#   docs: / test: / chore: / ci: / style:     → rien
#
# Écrit le tag sur stdout (« v1.4.0 ») et sort en 0. Si aucun commit ne
# justifie une release — parce que HEAD est déjà taggé, ou parce que le push
# ne contient que de la doc —, n'écrit rien et sort en 0 quand même : ce n'est
# pas une erreur, c'est un push qui ne mérite pas de version.
#
# Exige un clone complet avec les tags (actions/checkout fetch-depth: 0).

set -euo pipefail

# --match écarte les tags qui ne sont pas des versions (un éventuel
# « nightly » ou « latest » ne doit pas servir de base de calcul).
last_tag=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)

if [ -n "$last_tag" ]; then
	range="${last_tag}..HEAD"
	base="${last_tag#v}"
else
	# Dépôt sans aucun tag de version : on part de 0.0.0, le premier feat
	# donnera donc v0.1.0.
	range="HEAD"
	base="0.0.0"
fi

if ! [[ "$base" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
	echo "next-version: dernier tag inexploitable comme semver : $last_tag" >&2
	exit 1
fi
major="${BASH_REMATCH[1]}"
minor="${BASH_REMATCH[2]}"
patch="${BASH_REMATCH[3]}"

subjects=$(git log --format='%s' "$range")
bodies=$(git log --format='%B' "$range")

# Un « ! » avant le deux-points, quel que soit le type, marque une rupture ;
# « BREAKING CHANGE: » en tête de ligne du corps aussi (les deux formes sont
# normatives, on accepte le tiret comme la spec le tolère).
if grep -qE '^[a-z]+(\([^)]*\))?!:' <<<"$subjects" ||
	grep -qE '^BREAKING[ -]CHANGE:' <<<"$bodies"; then
	bump=major
elif grep -qE '^feat(\([^)]*\))?:' <<<"$subjects"; then
	bump=minor
elif grep -qE '^(fix|perf|refactor)(\([^)]*\))?:' <<<"$subjects"; then
	bump=patch
else
	bump=none
fi

case "$bump" in
major)
	major=$((major + 1))
	minor=0
	patch=0
	;;
minor)
	minor=$((minor + 1))
	patch=0
	;;
patch)
	patch=$((patch + 1))
	;;
none)
	exit 0
	;;
esac

echo "v${major}.${minor}.${patch}"
