#!/bin/sh
# Regenerates the GIFs of docs/assets/ from the tapes of demo/tapes/.
# Requires vhs (https://github.com/charmbracelet/vhs), cargo and python3.
#
#   demo/record.sh            # every tape
#   demo/record.sh hero       # only demo/tapes/hero.tape
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
cargo build --release

# Isolated from your own daemons and settings, which must not show up in a GIF.
# The path is short on purpose: a Unix socket path is limited to about 100 bytes.
tmp=$(mktemp -d /tmp/lzs.XXXXXX)
export XDG_RUNTIME_DIR="$tmp/run" XDG_STATE_HOME="$tmp/state" XDG_CONFIG_HOME="$tmp/config"
export PATH="$root/target/release:$PATH"
export DEMO_ROOT="$root/demo"
mkdir -p "$XDG_RUNTIME_DIR" "$XDG_STATE_HOME" "$XDG_CONFIG_HOME/lazyshell" docs/assets
echo 'language: en' > "$XDG_CONFIG_HOME/lazyshell/config.yaml"

stop_daemons() {
  for project in acme-shop blog; do
    lazyshell down "$DEMO_ROOT/$project" >/dev/null 2>&1 || true
  done
}
trap 'stop_daemons; rm -rf "$tmp"' EXIT
trap 'exit 1' INT TERM

[ $# -gt 0 ] || set -- hero logs panels
for tape in "$@"; do
  stop_daemons
  lazyshell up "$DEMO_ROOT/blog"
  # The hero tape shows the startup itself; the others begin on a project already running.
  if [ "$tape" != hero ]; then
    lazyshell up "$DEMO_ROOT/acme-shop"
    sleep 12
  fi
  vhs "demo/tapes/$tape.tape"
done
