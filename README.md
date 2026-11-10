# lazyshell

`lazyshell` runs the long-lived processes of a project (dev server, watcher, database, tests…) as
named sessions, described in a `lazyshell.yml` file, and lets you watch them in a two-pane TUI:
the session list on the left, the live output of the selected one on the right.

A background daemon owns the sessions. Closing the TUI does not stop them: reopen it later, or
drive them from scripts and AI agents with plain CLI subcommands.

> **v2 is a full rewrite in Rust.** The goal changed from a `tmux`-like terminal multiplexer
> (Go, versions `v1.x`) to a project process manager. The Go code and its history live on the
> `legacy/go` branch and the `v1.*` tags.

## Install

There are no prebuilt binaries yet. Build from source (Rust, edition 2024; Unix only — Linux and
macOS, because the daemon talks over a Unix socket):

```sh
git clone https://github.com/thomas-gleizes/lazyshell.git
cd lazyshell
cargo install --path .
lazyshell --help
```

## Quick start

Create a `lazyshell.yml` at the root of your project:

```yaml
sessions:
  - name: api
    group: backend
    cwd: ./api
    command: cargo run
    autostart: true
  - name: web
    group: frontend
    cwd: ./web
    command: npm run dev
    env:
      PORT: "5173"
```

Then run, from anywhere inside the project:

```sh
lazyshell
```

`lazyshell` looks for `lazyshell.yml` (or `lazyshell.yaml`) in the current directory, then in its
parents up to your home directory. You can also pass a file or a directory: `lazyshell path/to/dir`.
It starts the project daemon in the background if it is not already running.

## Configuration

### Project file (`lazyshell.yml`)

| Key | Where | Description |
|---|---|---|
| `shell` | root | Shell used to run commands. Default: `$SHELL`, else `/bin/sh`. |
| `env_file` | root, session | A path or a list of paths to `.env` files. Later files override earlier ones; a missing file is ignored. |
| `groups` | root | List of `- name: …` entries declaring session groups. |
| `sessions` | root | Required, at least one session. Names must be unique. |
| `name` | session | Session name. |
| `command` | session | Command to run. |
| `group` | session | Group the session belongs to, to start/stop/restart several at once. |
| `cwd` | session | Working directory, relative to the folder of the config file. |
| `env` | session | Inline variables. |
| `autostart` | session | Start the session when the daemon starts. Default: `false`. |

Environment precedence, lowest to highest: root `env_file` < session `env_file` < session `env`.

`.env` syntax: `KEY=value`, optional `export` prefix, `#` comments, double-quoted values (escapes
`\n \t \" \\`) or single-quoted values (literal). No interpolation.

### User settings

`$XDG_CONFIG_HOME/lazyshell/config.yaml` (default `~/.config/lazyshell/config.yaml`):

```yaml
language: en   # fr (default) or en
```

An unknown key or value is an error, so typos are not silently ignored.

## TUI keys

Press `?` in the TUI for the same list.

| Key | Action |
|---|---|
| `j` / `k`, `↑` / `↓` | Next / previous session |
| `s` / `x` / `r` | Start / stop / restart the session (stop = SIGTERM, then SIGKILL after 10 s) |
| `S` / `X` / `R` | Start / stop / restart the whole group |
| `PgUp` / `PgDn`, mouse wheel | Scroll the output |
| `g` / `G` | Top of history / back to live |
| `Tab`, click | Switch between the Output and Env tabs |
| `w` | Soft-wrap on/off |
| `←` / `→` | Scroll horizontally (wrap off) |
| `f` | Fullscreen output (`Esc` to leave) |
| `/`, `n` / `N` | Search the output, older / newer match (`Esc` clears) |
| mouse drag | Select text in the output panel and copy it to the clipboard |
| `L` | Reload the config (running sessions are left unchanged) |
| `q` | Quit the TUI, sessions keep running |
| `Q` | Stop all sessions and the daemon |

A `~` next to a session means its config changed: restart it to apply the change.

## Command line

All subcommands take an optional `[path]` (file or directory) to pick the project; the default is
the current directory.

| Command | Description |
|---|---|
| `lazyshell [path]` | Open the TUI (starts the daemon if needed). |
| `lazyshell status` | Sessions of the project: group, state, PID, CPU, memory, listening ports. |
| `lazyshell ps [-a]` | Sessions of **all** daemons, like `docker ps` (`-a` includes stopped ones). |
| `lazyshell start\|stop\|restart <name> [-g]` | Act on a session, or a group with `-g`. |
| `lazyshell kill [-a]` | Stop every running session of the project (`-a`: of all projects). |
| `lazyshell logs <name> [-f] [-n N] [--raw]` | Print the output as plain text; `-f` follows, `-n` limits to the last N lines, `--raw` keeps ANSI sequences. |
| `lazyshell env <name>` | Print the environment injected into a session (`KEY=value`). |
| `lazyshell reload` | Re-read the config and apply it to the running daemon. |
| `lazyshell shutdown` | Stop all sessions, then the daemon. |
| `lazyshell daemon [--auto-exit]` | Run the daemon in the foreground (the TUI normally spawns it). |

`logs` and `env` give scripts and AI agents a way to read a session without a terminal.

## How it works

- One daemon per project, identified by the absolute path of its config file.
- The daemon listens on a Unix socket under `$XDG_RUNTIME_DIR/lazyshell/` (or
  `/tmp/lazyshell-<uid>/`), in a directory with mode `0700`, so other users cannot reach it.
- Each session runs in its own pseudo-terminal; the output panel is a terminal emulator, so
  colours and full-screen programs render correctly.
- With `--auto-exit` (what the TUI uses), the daemon exits on its own once no session is active
  and no client is connected.

## License

[MIT](LICENSE)
