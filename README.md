# lazyshell

`lazyshell` is a `tmux`/`screen`-like terminal session manager with a
`lazygit`/`lazydocker`-style two-pane TUI: a list of your shell sessions on
the left, the live output of whichever one is selected on the right. Sessions
keep running in the background — with their scrollback preserved — even while
you're looking at a different one.

The output panel is a real terminal emulator, so full-screen applications
work: `vim`, `htop` and `less` run inside a session, cursor and colours
included.

![demo](docs/demo.gif)

🇫🇷 **Version française de ce document : [`docs/README.fr.md`](docs/README.fr.md).**

📖 **Online documentation: [thomas-gleizes.github.io/lazyshell](https://thomas-gleizes.github.io/lazyshell/)**
— installation, usage, configuration, AI agent sessions and project configuration, in
[French](https://thomas-gleizes.github.io/lazyshell/fr/) and in
[English](https://thomas-gleizes.github.io/lazyshell/en/). The site's sources live in
`site/`.

## Install

Prebuilt binary, no Go toolchain needed: download the archive for your OS/arch
from the [releases page](https://github.com/thomas-gleizes/lazyshell/releases),
check it against `checksums.txt`, then extract `lazyshell` into a directory on
your `PATH`:

```sh
tar xzf lazyshell_<os>_<arch>.tar.gz
sudo mv lazyshell /usr/local/bin/
lazyshell --version
```

With Go installed:

```sh
go install github.com/thomas-gleizes/lazyshell/cmd/lazyshell@latest
```

Or from source:

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
| `q` / `Ctrl+C` | Quit lazyshell |
| `Tab` | Switch the focused panel |
| `?` | Show the help |
| `j` / `↓` | Next session |
| `k` / `↑` | Previous session |
| `n` | New session |
| `x` / `d` | Kill the selected session |
| `D` | Delete the selected session for good (removed from the panel) |
| `r` | Rename the selected session |
| `c` | Duplicate the selected session |
| `N` | New session, asking for its name first (empty = automatic name) |
| `M` | New session in a directory you pick |
| `w` | Export the selected session's scrollback to a file |
| `b` | Mark/unmark the session for broadcast |
| `F12` | Show/hide the debug panel (only does something under `--debug`) |

While the **output panel** is focused, these apply instead:

| Key | Action |
| --- | --- |
| `i` / `Enter` | Hand the keyboard to the shell (pass-through mode) |
| `Ctrl+O` (configurable) | Take the keyboard back, out of pass-through mode |
| `Esc` `Esc` | Same, without a key to learn: two Escapes in a row, within 400 ms |
| `PgUp` / `PgDn` | Scroll one screen through the history |
| `Ctrl+U` / `Ctrl+D` | Scroll half a screen |
| `/` | Search the history; `n` / `N` for the next/previous match |
| `v` | Start (or extend) a line selection — copy mode |
| `y`, or a second `v` | Copy the selection (OSC 52, or the configured fallback command) |
| `Esc` | Leave the search, or cancel the selection in progress |

Starting a session (`n`, `N`, `c`) or restarting one (`R`) lands you straight
inside it: the output panel takes the focus and pass-through is armed, so you
can type immediately. `Ctrl+O` gets the keyboard back. Moving the selection
with `j` / `k` is navigation and never does this.

Two `Esc` in a row, within 400 ms of each other, get the keyboard back too —
the exit you can find without reading this table. It is a genuine double press:
the first `Esc` is forwarded to the session like any other key, so `Esc` keeps
working in `vim` and in an agent session, and any other key typed in between
breaks the pair. The one habit it does not survive is double-tapping `Esc` in
`vim` out of reflex, which will leave pass-through; `Ctrl+O` remains the exit
for anyone who would rather it did not.

A shell that ends on its own — `exit`, `Ctrl+D`, or whatever it was running
finishing — takes the interface with it: pass-through is disarmed and focus
goes back to the sessions panel, on that same session. It stays selected and
listed, exited, so `R` restarts it and `x` / `D` disposes of it. Nothing
happens behind a popup: a confirmation or the help keeps the focus it has.

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

### Mouse

On by default. Click a session to select it — that's navigation, so it does
*not* hand the keyboard to the shell; double-click does. The wheel scrolls the
output panel's content, and never the shell's command history: `lazyshell`
handles the wheel itself instead of letting the terminal turn it into arrow
keys, which at a prompt would recall the previous command instead of scrolling.
Click and drag to select lines, then `y` to copy — releasing the button copies
nothing on its own.

A program inside a session gets the mouse only once it asks for it (`vim` with
`set mouse=a`, `htop`); a shell or an AI agent CLI never asks, so the wheel
keeps scrolling the scrollback. Set `mouse.forward_to_app: false` to keep the
mouse for `lazyshell` regardless.

The one thing turning the mouse on costs: `Shift-Up` and `Shift-Down` are no
longer forwarded to a session. `gocui` gives those keys and the mouse buttons
the same values, so they cannot both work — see
[ADR 0003](docs/adr/0003-souris.md). Set `mouse.enabled: false` to get them
back, at the price of the gestures above.

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
| `sessions_panel_width` | int ≥ 5 | `40` | Sessions list width, in columns, in landscape mode. |
| `sessions_panel_height` | int ≥ 5 | `10` | Sessions list height, in rows, in portrait mode. |
| `portrait_max_width` | int | `84` | Portrait mode applies at or below this terminal width… |
| `portrait_min_height` | int | `45` | …and above this terminal height. Portrait stacks the panels instead of splitting them side by side. |
| `refresh_interval_ms` | int, 10–1000 | `30` | Redraw period. An unchanged panel is never pushed, so idle cost stays near zero at any value. |
| `kill_timeout_ms` | int ≥ 100 | `2000` | Wait after `SIGTERM` before escalating to `SIGKILL`, and again before giving up. |
| `prefix_key` | key spec | `Ctrl+O` | Pass-through escape key: one press, out. Must be a control key, and it can no longer be typed into a session. `$LAZYSHELL_PREFIX` overrides it. |
| `keybindings` | map | see below | Remaps an action id to a key spec. An action left out keeps its default key. |
| `markers.bell` | 0–1 char | `!` | Gutter marker for a session that rang while hidden. `""` turns it off. |
| `markers.alt_screen` | 0–1 char | `#` | Gutter marker for a session running a full-screen application. `""` turns it off. |
| `markers.activity` | 0–1 char | `●` | Gutter marker for a session that produced output while hidden. `""` turns it off. |
| `markers.broadcast` | 0–1 char | `+` | Gutter marker for a session marked to receive broadcast keystrokes. `""` turns it off. |
| `markers.agent_idle` | 0–1 char | `·` | Gutter marker for a detected AI agent session that is idle. `""` turns it off. |
| `markers.agent_working` | 0–1 char | `…` | Gutter marker for a detected AI agent session that is working. `""` turns it off. |
| `markers.agent_blocked` | 0–1 char | `‼` | Gutter marker for a detected AI agent session waiting on you. `""` turns it off. |
| `markers.agent_done` | 0–1 char | `✓` | Gutter marker for a detected AI agent session that finished its turn. `""` turns it off. |
| `scroll.page_lines` | int ≥ 0 | `0` | Lines `PgUp`/`PgDn` move by. `0` means one full panel height. |
| `scroll.half_page_divisor` | int ≥ 1 | `2` | `Ctrl-U`/`Ctrl-D` move by the panel height divided by this. |
| `theme.active_border_color` | color | `green` | Focused panel's border. |
| `theme.inactive_border_color` | color | `default` | Every other panel's border. |
| `theme.selected_bg_color` | color | `blue` | Selected line's background in the sessions list. |
| `theme.pass_through_border_color` | color | `red` | Focused panel's border while in pass-through mode. |
| `theme.tab_active_color` | color | `green` | Selected tab in the output panel's tab strip. |
| `clipboard.fallback_command` | string | `""` | Command run with the yanked text on its stdin, instead of OSC 52, for a terminal that does not support it. There is no way to detect support, so this is a manual switch: empty means OSC 52 only. |
| `notify.fallback_command` | string | `""` | Command run with the notification text on its stdin, instead of OSC 9/777, when a detected AI agent session goes blocked or done. Empty means OSC only. |
| `window_title.enabled` | bool | `true` | Whether the host terminal's window/tab title tracks the focused session (its name, plus its live OSC 0/2 title when one is set) via OSC 0. |
| `mouse.enabled` | bool | `true` | Click, wheel and drag support. Turning it on costs `Shift-Up`/`Shift-Down` pass-through — gocui gives those keys and the mouse buttons the same values, so they cannot both work. Set to `false` to get them back. |
| `mouse.wheel_lines` | int ≥ 1 | `3` | Lines one wheel notch scrolls the output panel by. |
| `mouse.forward_to_app` | bool | `true` | Whether a program inside a session may receive the mouse itself, and only once it has asked for it with a DECSET 9/1000/1002/1003 (`vim` with `set mouse=a`, `htop`). A shell or an AI agent CLI never asks, so the wheel keeps scrolling lazyshell's scrollback. |
| `perf.refresh_interval_ms` | `0`, or int ≥ 100 | `5000` | How often every session's processes are sampled for the resources tab. This runs in the background whether or not that tab is open, so its curves already go back further than the moment you opened them; all sessions are sampled in one pass, so the cost does not grow with their number. `0` turns sampling off entirely — it is the one periodic job that spawns a process, so someone who never opens the tab need not pay for it. |
| `env_tab.mask_secrets` | bool | `true` | Whether the output panel's env tab masks the value of variables whose name looks like a credential (`TOKEN`, `SECRET`, `PASSWORD`, `AUTH`, `..._KEY`). The panel is as shareable as a screenshot of it; set to `false` to see the real values. |
| `agent_stats_command` | string | `""` | Run for the selected AI agent session, with `$LAZYSHELL_SESSION_ID` in its environment; its first line of stdout is shown next to the turn duration. Empty disables it. |

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

The remappable action ids are `new_session`, `new_named_session`, `new_session_in_dir`,
`kill_session`, `delete_session`, `rename_session`, `duplicate_session`,
`restart_session`, `zoom`, `filter_sessions`, `export_session`,
`toggle_broadcast`, `jump_next_blocked`, `next_tab`, `prev_tab`,
`toggle_debug`, `select_next`, `select_prev`,
`cycle_focus`, `help` and `quit`. An id outside
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

sessions_panel_width: 40
sessions_panel_height: 10
portrait_max_width: 84
portrait_min_height: 45

refresh_interval_ms: 30
kill_timeout_ms: 2000

prefix_key: Ctrl+O

keybindings:
  new_session: "n"
  new_named_session: "N"
  new_session_in_dir: "M"
  kill_session: "x"
  delete_session: "D"
  rename_session: "r"
  duplicate_session: "c"
  restart_session: "R"
  zoom: "z"
  next_tab: "]"
  prev_tab: "["
  filter_sessions: "/"
  export_session: "w"
  toggle_broadcast: "b"
  jump_next_blocked: "B"
  toggle_debug: F12
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
  agent_idle: "·"
  agent_working: "…"
  agent_blocked: "‼"
  agent_done: "✓"

scroll:
  page_lines: 0
  half_page_divisor: 2

theme:
  active_border_color: green
  inactive_border_color: default
  selected_bg_color: blue
  pass_through_border_color: red
  tab_active_color: green

clipboard:
  fallback_command: ""

notify:
  fallback_command: ""

window_title:
  enabled: true

mouse:
  enabled: true
  wheel_lines: 3
  forward_to_app: true

perf:
  refresh_interval_ms: 5000

env_tab:
  mask_secrets: true

agent_stats_command: ""
```

### AI agent sessions

A session whose foreground process is a known AI coding agent CLI (`claude`,
`codex`, `opencode`) gets a gutter marker showing its detected state — `idle`,
`working`, `blocked` (waiting on you) or `done` — instead of only the generic
activity marker, which cannot tell "it produced output" from "it wants an
answer". Detection needs no configuration: it reads the built-in manifests
under `pkg/agent/manifests` against the session's visible screen and terminal
title.

Drop a `<process-name>.yml` file in `~/.config/lazyshell/agents/` (or your
`$XDG_CONFIG_HOME` equivalent) to override a built-in manifest or add one for
another agent — same file name as a built-in replaces it outright, a
different name adds to the set. See the built-in manifests for the format.
Manifests are local only; lazyshell never fetches one over the network.

#### Authoritative state via hooks

Manifest detection is a guess from what is on screen — a second channel lets
the agent say its state outright instead. Every session gets its own Unix
socket, exposed to the process running inside it as `$LAZYSHELL_SOCK`
(alongside `$LAZYSHELL_SESSION_ID`), and `lazyshell hook <state>` — one of
`idle`, `working`, `blocked` or `done` — writes to it. It is meant to be
wired into the agent's own hook mechanism, not typed by hand:

```sh
lazyshell init --agents   # prints the config to paste into Claude Code / Codex
```

**Claude Code** — a `settings.json` hooks block: `UserPromptSubmit` →
`lazyshell hook working`, `Notification` → `lazyshell hook blocked`, `Stop` →
`lazyshell hook done`. **Codex** — a `notify` line in `config.toml`; Codex has
only one event (`agent-turn-complete`), so it can only ever report `done`.
**opencode** is not wired up yet — its richest signal is an SSE subscription
rather than something it pushes on its own, a different shape of integration
left for later.

Once a session has received a single hook event, the manifest-based guessing
stops for that session for good: the hook is authoritative from then on, not
just until the next screen change. lazyshell never calls the agent through
this socket — it only ever listens, and the only thing a hook event can do is
set that one state.

#### Notifications, jumping to what's waiting, and turn stats

A session going `blocked` or `done` fires a desktop notification — OSC 9 and
OSC 777 to the host terminal by default (both sent unconditionally; a
terminal that does not understand one just ignores it), or the command in
`notify.fallback_command` instead, with the notification text on its stdin,
for a terminal that needs one. At more than a couple of agent sessions open,
`B` jumps the selection straight to the next `blocked` one, cycling and
wrapping — the point of the marker and the notification both.

A session currently mid-turn (`working`) shows how long its turn has been
running in the sessions list, e.g. `⏱ 1m32s`. Setting `agent_stats_command`
runs that command for the *selected* session only (at most once every 5
seconds — it is meant for something like a token/cost lookup, not something
cheap enough to run per session on every tick) with `$LAZYSHELL_SESSION_ID`
in its environment, and shows its first line of output next to the
duration — the same "external command, show its output line" shape as
Claude Code's own `statusLine`. lazyshell does not parse or track token
usage itself.

## Project configuration

Run `lazyshell` in a directory holding a `lazyshell.yml` and it starts the
sessions that file declares — each in its own directory, with its own
environment and command — instead of coming up empty.

`lazyshell init` writes a commented starting point in the current directory.

```yaml
# ./lazyshell.yml

# Optional: overrides the user config's shell, for this project only.
shell: /bin/zsh

# Optional: .env files loaded for every session below, in order — a later
# file overrides a key set by an earlier one.
env_files:
  - .env
  - .env.local

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
    # Optional: on top of env_files above, for this session only.
    env_files:
      - .env.api

  - name: web
    cwd: ./web
    command: npm run dev

  - name: shell          # no command: a plain shell in the project directory
```

Sessions start in file order, and the first one is selected. An entry that
does not validate (empty or duplicate `name`, missing `cwd`) is skipped and
reported in the status bar — the others still start.

**Only `shell`, `env_files`, `no_default_env` and `sessions` are read from a
project file.** `theme`, `keybindings`, `prefix_key` and the rest stay under
your control alone: a repository you cloned must not be able to remap your
keyboard. Other keys are ignored, with a warning on stderr.

### .env files

Every session — declared in a project file or not — automatically loads a
`.env` from its own working directory, if there is one. Layered on top, each
overriding a key the previous layer set:

1. `<session cwd>/.env`, automatic, unless disabled (see below)
2. `--env-file <path>` (repeatable, applies to every session this run starts)
3. the project's own `env_files:` (applies to every declared session)
4. a session's own `env_files:` (that session only)
5. that session's `env:` map — always wins, over every file

To stop the automatic `<cwd>/.env` lookup, pass `--no-env-file` (every
session this run starts), set `no_default_env: true` at the top of a project
file (every session it declares), or on one `SessionSpec` (that session
only — overriding the project's own setting in either direction).

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
lazyshell --env-file .env.prod   # extra .env file, for every session this run starts
lazyshell --no-env-file          # skip every session's automatic "<cwd>/.env"
```

Approvals live in `trust.yml` next to your user config. When stdin is not a
terminal, approval is refused rather than assumed.

## Debug mode

Once lazyshell owns the terminal there is nowhere left to print: stderr is
gone and the status bar is one line. `--debug` is the way to see what the
interface thinks is happening.

```sh
lazyshell --debug
```

It does two things at once. It appends to
`~/.config/lazyshell/debug.log` — next to `config.yml`, `0600`, never
truncated, so two runs can be compared — and it opens a small panel in the
output panel's top-right corner showing the last events live. `F12` hides and
shows that panel; the file keeps being written either way.

Three kinds of line are recorded:

| Tag | What it is |
| --- | --- |
| `KEY` | A keystroke as the output panel received it: its name, the raw `key`/`ch`/`mod` values, what `Normalize` made of them when the two differ, and which mode it landed in (pass-through, scroll, copy-mode, search, a tab) |
| `ACT` | An action that fired — a keybinding, a mouse gesture, or one of the branches the output panel's editor handles itself |
| `EVT` | Session created / killed / exited, agent state transitions, selection and tab changes, panel resizes |

Two things worth knowing before you read a log:

- **Keys are only recorded for the output panel.** gocui offers no global
  keyboard hook, so a key pressed on the sessions panel shows up as an `ACT`
  line if it is bound, and not at all if it is not.
- **`F12` no longer reaches the session** while lazyshell is running, debug
  mode or not — it is a global binding. Remap `toggle_debug` in your config if
  something you run needs it.

The log contains every keystroke typed into a shell, including at a password
prompt of a program that does not turn echo off. It is written `0600` for that
reason; delete it when you are done with it.

## Development

```sh
make build   # go build -o bin/lazyshell ./cmd/lazyshell
make test    # go test -race ./...
make lint    # golangci-lint run
```
