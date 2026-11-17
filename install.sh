#!/bin/sh
# Installe lazyshell depuis la dernière release GitHub.
#
#   curl -fsSL https://raw.githubusercontent.com/thomas-gleizes/lazyshell/main/install.sh | sh
#
# Variables d'environnement :
#   LAZYSHELL_VERSION      tag à installer (ex. v0.2.0) ; par défaut, la dernière release
#   LAZYSHELL_INSTALL_DIR  dossier d'installation ; par défaut ~/.local/bin
set -eu

REPO="thomas-gleizes/lazyshell"
INSTALL_DIR="${LAZYSHELL_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${LAZYSHELL_VERSION:-latest}"

err() { printf 'erreur : %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "« $1 » est requis mais introuvable"; }

need curl
need tar
need uname

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) err "système non supporté : $(uname -s) (Linux et macOS uniquement)" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) err "architecture non supportée : $(uname -m)" ;;
esac

archive="lazyshell_${os}_${arch}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  base="https://github.com/${REPO}/releases/latest/download"
else
  base="https://github.com/${REPO}/releases/download/${VERSION}"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Téléchargement de ${archive} (${VERSION})…"
curl -fsSL "${base}/${archive}" -o "${tmp}/${archive}" \
  || err "téléchargement impossible : ${base}/${archive}"

# Vérification de l'intégrité via checksums.txt, publié avec la release.
if curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt"; then
  expected="$(grep " ${archive}\$" "${tmp}/checksums.txt" | cut -d' ' -f1)"
  [ -n "$expected" ] || err "${archive} absent de checksums.txt"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${tmp}/${archive}" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "${tmp}/${archive}" | cut -d' ' -f1)"
  else
    err "ni sha256sum ni shasum disponible pour vérifier l'archive"
  fi
  [ "$expected" = "$actual" ] || err "somme de contrôle invalide pour ${archive}"
else
  err "checksums.txt introuvable : impossible de vérifier l'archive"
fi

tar -xzf "${tmp}/${archive}" -C "$tmp" lazyshell
mkdir -p "$INSTALL_DIR"
install -m 755 "${tmp}/lazyshell" "${INSTALL_DIR}/lazyshell"

echo "Installé : ${INSTALL_DIR}/lazyshell ($("${INSTALL_DIR}/lazyshell" --version))"
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Ajoute ${INSTALL_DIR} à ton PATH :  export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
