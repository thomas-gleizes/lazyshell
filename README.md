# lazyshell

`lazyshell` is a `tmux`/`screen`-like terminal session manager with a
`lazygit`/`lazydocker`-style two-pane TUI: a list of your shell sessions on
the left, the live output of whichever one is selected on the right. Sessions
keep running in the background — with their scrollback preserved — even while
you're looking at a different one.

The output panel is a real terminal emulator, so full-screen applications
work: `vim`, `htop` and `less` run inside a session, cursor and colours
included.

<!-- TODO: demo gif -->

## Install

```sh
go install github.com/thomas-gleizes/lazyshell/cmd/lazyshell@latest
```

Or build from source:

```sh
git clone https://github.com/thomas-gleizes/lazyshell.git
cd lazyshell
make build   # produces ./bin/lazyshell
```

## Usage

Run `lazyshell` in a terminal. `Tab` switches focus between the sessions
panel and the output panel; while the output panel is focused, keystrokes go
straight to that session's shell ("pass-through" mode). Press `?` at any time
to open an in-app help popup listing every binding below.

| Key | Action |
| --- | --- |
| `q` / `Ctrl+C` | Quitter lazyshell |
| `Tab` | Changer de panneau actif |
| `?` | Afficher l'aide |
| `j` / `↓` | Session suivante |
| `k` / `↑` | Session précédente |
| `n` | Nouvelle session |
| `x` / `d` | Tuer la session sélectionnée |
| `r` | Renommer la session sélectionnée |
| `c` | Dupliquer la session sélectionnée |
| `N` | Nouvelle session dans un dossier choisi |
| `Ctrl+B` (configurable) | Prefix used to escape pass-through mode back to the sessions panel |

### Reading the sessions list

Each session is one line: a two-column gutter, then its name, status, PID, and
either the terminal title the shell set (usually the running command) or its
working directory.

| Marker | Meaning |
| --- | --- |
| `!` | The session rang the bell while you were looking elsewhere. Cleared when you select it. |
| `#` | A full-screen application (`vim`, `htop`, `less`) has the session. Shown as `[ALT]` in the status bar for the selected one. |

While a full-screen application is in control, scrolling back through history
is disabled — the alternate screen does not feed the scrollback, and those keys
belong to the application. `lazyshell` never switches mode on its own: use `i`
or `Enter` to give the keyboard to the shell, and the prefix key to take it
back.

## Configuration

`lazyshell` reads a YAML config file from (in order of precedence):

1. `$LAZYSHELL_CONFIG`, if set
2. `$XDG_CONFIG_HOME/lazyshell/config.yml`
3. `~/.config/lazyshell/config.yml`

A missing file is not an error — lazyshell just runs with its built-in
defaults. A partial file only needs to mention the fields it wants to
override; everything else keeps its default.

```yaml
# ~/.config/lazyshell/config.yml

# Command started behind each new session's pty. Empty means "$SHELL,
# falling back to /bin/bash".
shell: ""

# Maximum number of lines a session's terminal emulator keeps once they
# scroll off-screen.
scrollback_size: 10000

# Sessions list's width in landscape mode (columns), height in portrait
# mode (rows).
sessions_panel_width: 30

# Pass-through escape prefix, in gocui.Parse syntax. Overridable at
# runtime via $LAZYSHELL_PREFIX, which wins over this value.
prefix_key: "Ctrl+B"

# Remap any action below to another key (gocui.Parse syntax, e.g.
# "Ctrl+N" or "Alt+Space"). Actions left out keep their default key.
keybindings:
  new_session: "n"
  kill_session: "x"
  rename_session: "r"
  duplicate_session: "c"
  new_session_in_dir: "N"
  select_next: "j"
  select_prev: "k"
  cycle_focus: "Tab"
  help: "?"
  quit: "q"

# Every color accepts a W3C color name (gocui.GetColor's syntax) or
# "default" to use the terminal's own color.
theme:
  active_border_color: "green"
  inactive_border_color: "default"
  selected_bg_color: "blue"
  pass_through_border_color: "red"
```

## Development

```sh
make build   # go build -o bin/lazyshell ./cmd/lazyshell
make test    # go test -race ./...
make lint    # golangci-lint run
```
