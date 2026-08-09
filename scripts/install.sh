#!/usr/bin/env bash
# Downloads the latest lazyshell release from GitHub, verifies it against
# checksums.txt, and extracts the lazyshell binary onto the local filesystem.
# See README.md#install for the other install methods (go install, from source).
set -euo pipefail

REPO="thomas-gleizes/lazyshell"
INSTALL_DIR="${LAZYSHELL_INSTALL_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
    linux|darwin) ;;
    *) echo "error: unsupported OS '$os' (lazyshell ships linux and darwin builds only)" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "error: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

archive="lazyshell_${os}_${arch}.tar.gz"
base_url="https://github.com/${REPO}/releases/latest/download"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

echo "Downloading ${archive}..."
curl -fL --progress-bar -o "${workdir}/${archive}" "${base_url}/${archive}"
curl -fL --progress-bar -o "${workdir}/checksums.txt" "${base_url}/checksums.txt"

echo "Verifying checksum..."
(cd "$workdir" && sha256sum --ignore-missing -c checksums.txt)

echo "Extracting..."
tar xzf "${workdir}/${archive}" -C "$workdir" lazyshell

if [ -w "$INSTALL_DIR" ]; then
    mv "${workdir}/lazyshell" "${INSTALL_DIR}/lazyshell"
else
    sudo mv "${workdir}/lazyshell" "${INSTALL_DIR}/lazyshell"
fi

echo "Installed to ${INSTALL_DIR}/lazyshell"
"${INSTALL_DIR}/lazyshell" --version
