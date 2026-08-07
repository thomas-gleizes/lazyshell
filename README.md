# lazyshell

`lazyshell` is a `tmux`/`screen`-like terminal session manager with a
`lazygit`/`lazydocker`-style two-pane TUI: a list of your shell sessions on
the left, the live output of whichever one is selected on the right. Sessions
keep running in the background — with their scrollback preserved — even while
you're looking at a different one.

The output panel is a real terminal emulator, so full-screen applications
work: `vim`, `htop` and `less` run inside a session, cursor and colours
included.

![démo](docs/demo.gif)

## Install

Binaire précompilé, sans toolchain Go : télécharger l'archive de son OS/arch
depuis la [page des releases](https://github.com/thomas-gleizes/lazyshell/releases),
vérifier `checksums.txt`, puis extraire `lazyshell` dans un dossier de son `PATH` :

```sh
tar xzf lazyshell_<os>_<arch>.tar.gz
sudo mv lazyshell /usr/local/bin/
lazyshell --version
```

Avec Go installé :

```sh
go install github.com/thomas-gleizes/lazyshell/cmd/lazyshell@latest
```

Ou depuis les sources :

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
| `w` | Exporter le scrollback de la session sélectionnée vers un fichier |
| `b` | Marquer/démarquer la session pour la diffusion (broadcast) |

While the **output panel** is focused, these apply instead:

| Key | Action |
| --- | --- |
| `i` / `Enter` | Donner le clavier au shell (mode pass-through) |
| `Ctrl+B` (configurable) | Reprendre le clavier, depuis le mode pass-through |
| `PgUp` / `PgDn` | Défiler d'un écran dans l'historique |
| `Ctrl+U` / `Ctrl+D` | Défiler d'un demi-écran |
| `/` | Rechercher dans l'historique ; `n` / `N` pour l'occurrence suivante/précédente |
| `v` | Démarrer (ou étendre) une sélection de lignes — mode copie |
| `y` ou un second `v` | Copier la sélection (OSC 52, ou la commande de repli configurée) |
| `Esc` | Quitter la recherche ou annuler la sélection en cours |

Each panel also carries its own most-used keys on the bottom line of its
frame, so the common ones are readable without opening `?`. The list shortens
to whatever fits the panel's width, and the output panel's adapts to what it
is doing: only the way out of pass-through while pass-through is on, no
scrolling hint while a full-screen application has the session.

### Reading the sessions list

Each session is one line: a four-column gutter, then its name, status, PID,
and either the terminal title the shell set (usually the running command) or
its working directory.

| Marker | Meaning |
| --- | --- |
| `!` | The session rang the bell while you were looking elsewhere. Cleared when you select it. |
| `#` | A full-screen application (`vim`, `htop`, `less`) has the session. Shown as `[ALT]` in the status bar for the selected one. |
| `●` | The session produced output while it wasn't the one on screen. Cleared when you select it. |
| `+` | The session is marked for broadcast — see below. |

### Broadcast

Mark two or more sessions with `b`, then attach to any one of them (`i` /
`Enter`): every keystroke now goes to all of them at once, not just the one
you're looking at. The status bar carries a `⚠ BROADCAST → N sessions`
warning the whole time it is armed, in front of whatever else it would
otherwise say — this is the one state where a keystroke you don't expect to
matter can reach several shells behind your back, so it stays visible no
matter what. Unmark a session (`b` again) to drop it out; broadcasting stops
on its own once fewer than two remain marked.

While a full-screen application is in control, scrolling back through history
— and copy-mode, which selects out of that same history — is disabled: the
alternate screen does not feed the scrollback, and those keys belong to the
application. `lazyshell` never switches mode on its own: use `i`
or `Enter` to give the keyboard to the shell, and the prefix key to take it
back.

## Configuration

Run `lazyshell config init` to write a fully commented config file at the right
place, and `lazyshell config show` to print the configuration actually in
effect — after every layer below has had its say — together with the sources it
came from. That second command is the answer to "why is my setting not taking".

`lazyshell` reads its YAML config file from (first match wins):

1. `$LAZYSHELL_CONFIG`, if set
2. `$XDG_CONFIG_HOME/lazyshell/config.yml`
3. `~/.config/lazyshell/config.yml`

A missing file is not an error — lazyshell just runs with its built-in
defaults. A partial file only needs to mention the fields it wants to
override; everything else keeps its default.

Precedence, weakest to strongest:

```
built-in defaults  <  ~/.config/lazyshell/config.yml  <  project lazyshell.yml
                   <  environment variables  <  command-line flags
```

Nothing in a config file can stop lazyshell from starting. A key it does not
know, a value out of range, an unparseable keybinding or an unknown color are
each reported on stderr before the interface opens, and the built-in default is
used instead — never a silent no-op, never a refusal to run.

### Reference

| Key | Type | Default | Effect |
| --- | --- | --- | --- |
| `language` | `fr` \| `en` | `fr` | UI language: bindings, popups, status bar, footers and session messages. CLI output (`lazyshell config ...`) stays French. |
| `shell` | string | `""` | Command started behind each session's pty. Empty means `$SHELL`, falling back to `/bin/bash`. |
| `term` | string | `xterm-256color` | `TERM` announced to sessions. Lower it to make programs degrade on purpose. |
| `scrollback_size` | int ≥ 0 | `10000` | Lines kept per session once they scroll off-screen. |
| `sessions_panel_width` | int ≥ 5 | `30` | Sessions list width, in columns, in landscape mode. |
| `sessions_panel_height` | int ≥ 5 | `10` | Sessions list height, in rows, in portrait mode. |
| `portrait_max_width` | int | `84` | Portrait mode applies at or below this terminal width… |
| `portrait_min_height` | int | `45` | …and above this terminal height. Portrait stacks the panels instead of splitting them side by side. |
| `refresh_interval_ms` | int, 10–1000 | `30` | Redraw period. An unchanged panel is never pushed, so idle cost stays near zero at any value. |
| `kill_timeout_ms` | int ≥ 100 | `2000` | Wait after `SIGTERM` before escalating to `SIGKILL`, and again before giving up. |
| `prefix_key` | key spec | `Ctrl+B` | Pass-through escape prefix. Must be a control key. `$LAZYSHELL_PREFIX` overrides it. |
| `keybindings` | map | see below | Remaps an action id to a key spec. An action left out keeps its default key. |
| `markers.bell` | 0–1 char | `!` | Gutter marker for a session that rang while hidden. `""` turns it off. |
| `markers.alt_screen` | 0–1 char | `#` | Gutter marker for a session running a full-screen application. `""` turns it off. |
| `markers.activity` | 0–1 char | `●` | Gutter marker for a session that produced output while hidden. `""` turns it off. |
| `markers.broadcast` | 0–1 char | `+` | Gutter marker for a session marked to receive broadcast keystrokes. `""` turns it off. |
| `scroll.page_lines` | int ≥ 0 | `0` | Lines `PgUp`/`PgDn` move by. `0` means one full panel height. |
| `scroll.half_page_divisor` | int ≥ 1 | `2` | `Ctrl-U`/`Ctrl-D` move by the panel height divided by this. |
| `theme.active_border_color` | color | `green` | Focused panel's border. |
| `theme.inactive_border_color` | color | `default` | Every other panel's border. |
| `theme.selected_bg_color` | color | `blue` | Selected line's background in the sessions list. |
| `theme.pass_through_border_color` | color | `red` | Focused panel's border while in pass-through mode. |
| `clipboard.fallback_command` | string | `""` | Command run with the yanked text on its stdin, instead of OSC 52, for a terminal that does not support it. There is no way to detect support, so this is a manual switch: empty means OSC 52 only. |

Key specs use `gocui.Parse` syntax: a bare character (`n`), or `Ctrl+N`,
`Alt+Space`, `Tab`, `Esc`.

Colors accept any of:

- an **ANSI terminal color name** — `black`, `red`, `green`, `yellow`, `blue`,
  `magenta`, `cyan`, `white`, and each one prefixed with `bright`
  (`brightblue`, …). These mean what they mean in a terminal, and follow your
  terminal's own palette.
- a **W3C/CSS color name** (`navy`, `teal`, `chartreuse`, …) or `#rrggbb`, for
  a specific color rather than a palette slot.
- `default`, for the terminal's own default color.

The two name sets overlap and disagree: in CSS, `blue` is `#0000FF`, which a
terminal shows as *bright* blue. lazyshell resolves the ANSI names first, so
`blue` gives you ordinary blue; write `navy` if you want the CSS one, or
`brightblue` for the bright terminal slot.

The remappable action ids are `new_session`, `new_session_in_dir`,
`kill_session`, `rename_session`, `duplicate_session`, `restart_session`,
`zoom`, `filter_sessions`, `export_session`, `toggle_broadcast`,
`select_next`, `select_prev`, `cycle_focus`, `help` and `quit`. An id outside
that list is reported rather than ignored.

### Example

This is what `lazyshell config init` writes — every option at its default
value, so you can delete whatever you do not change.

```yaml
# ~/.config/lazyshell/config.yml

language: fr
shell: ""
term: xterm-256color
scrollback_size: 10000

sessions_panel_width: 30
sessions_panel_height: 10
portrait_max_width: 84
portrait_min_height: 45

refresh_interval_ms: 30
kill_timeout_ms: 2000

prefix_key: Ctrl+B

keybindings:
  new_session: "n"
  new_session_in_dir: "N"
  kill_session: "x"
  rename_session: "r"
  duplicate_session: "c"
  restart_session: "R"
  zoom: "z"
  filter_sessions: "/"
  export_session: "w"
  toggle_broadcast: "b"
  select_next: "j"
  select_prev: "k"
  cycle_focus: Tab
  help: "?"
  quit: "q"

markers:
  bell: "!"
  alt_screen: "#"
  activity: "●"
  broadcast: "+"

scroll:
  page_lines: 0
  half_page_divisor: 2

theme:
  active_border_color: green
  inactive_border_color: default
  selected_bg_color: blue
  pass_through_border_color: red

clipboard:
  fallback_command: ""
```

## Project configuration

Run `lazyshell` in a directory holding a `lazyshell.yml` and it starts the
sessions that file declares — each in its own directory, with its own
environment and command — instead of coming up empty.

`lazyshell init` writes a commented starting point in the current directory.

```yaml
# ./lazyshell.yml

# Optional: overrides the user config's shell, for this project only.
shell: /bin/zsh

sessions:
  - name: api
    # Relative to *this file*, not to where you launched lazyshell from.
    # `~` is expanded. Left out, it means this file's own directory.
    cwd: ./services/api
    # Typed into the shell once it is up, not exec'd in its place: when the
    # command exits (or you Ctrl-C it), the shell is still there.
    command: make dev
    env:
      PORT: "3000"

  - name: web
    cwd: ./web
    command: npm run dev

  - name: shell          # no command: a plain shell in the project directory
```

Sessions start in file order, and the first one is selected. An entry that
does not validate (empty or duplicate `name`, missing `cwd`) is skipped and
reported in the status bar — the others still start.

**Only `shell` and `sessions` are read from a project file.** `theme`,
`keybindings`, `prefix_key` and the rest stay under your control alone: a
repository you cloned must not be able to remap your keyboard. Other keys are
ignored, with a warning on stderr.

### Which file is used

1. `--config-file <file>` (`-f`)
2. `$LAZYSHELL_PROJECT_CONFIG`
3. `./lazyshell.yml`
4. `./.lazyshell.yml`

Only the current directory is searched — no walking up to a repository root,
so the file that runs is always the one you can see.

### Approving a project file

A `lazyshell.yml` is versioned in a repository, so it would otherwise run
arbitrary commands the moment you `cd` into a clone. lazyshell asks once, before
the interface opens, and remembers the answer per file — and asks again as soon
as the file's content changes:

```sh
lazyshell allow            # approve the current directory's file, launch nothing
lazyshell allow ./x.yml    # approve a specific file
lazyshell --no-autostart   # open the interface without starting anything
```

Approvals live in `trust.yml` next to your user config. When stdin is not a
terminal, approval is refused rather than assumed.

## Development

```sh
make build   # go build -o bin/lazyshell ./cmd/lazyshell
make test    # go test -race ./...
make lint    # golangci-lint run
```
