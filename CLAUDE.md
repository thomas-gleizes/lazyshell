# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

`lazyshell` is implemented and functional, past v1.1. Everything the phased roadmap planned, through
phase 13 ("onglets du panneau de sortie"), is built. This is an active Go codebase with a real
`go.mod`, CI, goreleaser packaging, and a test suite — do not treat this as a design-stage repo.

The *phased* `ROADMAP.md` no longer exists (removed in `04e4d9c`): the phase-by-phase history it
carried is in the git log, and what it said about the architecture lives in the two sections below.
Code comments and ADRs still cite phase numbers ("phase 11b", "the phase-1 spike") — those are
historical coordinates into that history, not a pointer to a file you can open.

The `ROADMAP.md` that exists today is a *forward-looking* one: candidate features, each with a
status (`idée` / `à concevoir` / `en cours` / `fait` / `abandonné`). It carries no history and is
not the record of what is built — the code is. Keep the statuses honest when work lands there, and
note that "Open items" below stays the reference for what was already decided and why.

`docs/repports/RAPPORT_ANALYSE_LAZYGIT_LAZYDOCKER.md` and
`docs/repports/RAPPORT_ANALYSE_INTEGRATION_AGENTS_IA.md` are historical design docs (the first drove
phases 0–10, the second drove phase 11). They record what was decided then, so read them as history:
the code is the authority on what is actually built.

## Project goal

`lazyshell` is a **TUI shell session manager** (a tmux/screen-like multiplexer), built in Go on top
of `gocui`. It gives a `lazygit`/`lazydocker`-style two-pane interface — a session list on the left,
live session output on the right — for managing multiple persistent shell sessions, including
long-running AI coding agent sessions (Claude Code, etc.) whose blocked/working/done state is
surfaced in the sessions panel.

## Key architectural decisions (all implemented)

- **Base library**: `github.com/jesseduffield/gocui`.
- **Layout engine**: `github.com/jesseduffield/lazycore/pkg/boxlayout` for the sessions-list /
  output-panel split, with portrait-mode stacking on narrow terminals.
- **Keybindings**: flat `Binding{ViewName, Key, Modifier, Handler}` + `g.SetKeybinding`
  (lazydocker-style) — no lazygit-style controller/context-stack pattern.
- **Async task management**: `pkg/tasks` (`TaskManager`) owns only *display/reading* goroutines —
  never the underlying shell process. Switching session selection cancels the reader/renderer for
  the previous session, not the pty/shell itself, which keeps running regardless of what's on screen.
- **PTY handling**: `github.com/creack/pty`, one pty per session, `pty.Setsize` propagated from the
  panel's computed layout size on resize, a full terminal emulator (not just ANSI stripping — see
  `docs/adr/0002-rendu-multi-panneaux.md`) so full-screen apps (`vim`, `htop`, `less`) work inside a
  session.
- **Process lifecycle**: `pkg/session.Manager` owns the `map[sessionID]*Session` and keeps shells
  alive across selection changes, decoupled from `pkg/tasks`.
- **AI agent sessions** (`pkg/agent`, `pkg/hook`): agent state (`working`/`blocked`/`done`) is
  detected either config-free (phase 11a) or via an authoritative hooks channel (phase 11b, e.g.
  Claude Code hooks) that overrides heuristic detection when present. Notifications on
  blocked/done go out via OSC 9 / OSC 777 to the host terminal (not `notify-send`), with an
  optional external fallback command. Nothing in `pkg/session` or `pkg/gui` outside
  `notify.go`/`stats.go` knows about agent-specific formats — the coupling is fully contained in
  `pkg/agent`/`pkg/hook`.

## Package layout (as built)

```
cmd/
  lazyshell/      main entrypoint
  spike-pty/      phase-1 pty spike (kept for reference, not part of the shipped binary)
pkg/
  app/            bootstrap: load config, build SessionManager, run gui.Run()
  session/        SessionManager: CRUD (New, Kill, List); Session{cmd, ptmx, scrollback, status};
                  Env() (launch-time) and Stats() (per-OS CPU/RSS/disk sampling)
  screen/         terminal emulator backing the output panel (vim/htop/less support)
  gui/            gocui init, layout, keybindings, mouse, panels, tabs, help, theme, notify, stats,
                  debug_panel/debug_trace (the --debug overlay and its instrumentation points)
  tasks/          TaskManager (display/reading goroutines only)
  agent/          AI agent state detection (config-free + hooks-driven)
  hook/           authoritative hooks channel for agent sessions
  debug/          --debug's recorder: append-only log file + ring of recent entries. A nil
                  *Logger is the "off" state and every method is nil-safe, which is what lets
                  pkg/gui call gui.debug.Key/Action/Event with no guard at the call site
  config/         user config + project config (`lazyshell.yml`) loading
  update/         `lazyshell update`: latest GitHub release → download → checksum → atomic
                  swap of the running binary (the in-binary scripts/install.sh)
  keys/           keybinding definitions
  i18n/           strings/translations
  version/        --version metadata (goreleaser-injected)
docs/
  README.fr.md    French translation of the root README
  adr/            architecture decision records (0001: rendu ANSI et clavier, 0002: rendu
                  multi-panneaux, 0003: souris, 0004: sortie du pass-through — remplace la
                  décision 3 de l'ADR 0001, 0005: `Esc` `Esc` comme seconde sortie — complète
                  l'ADR 0004 sans le remplacer, 0007: groupes de sessions — remplace l'invariant
                  « une ligne = une session » et étend l'ADR 0006, 0008: intégration shell OSC 133
                  — bornes de prompt/commande survivant à la troncature du scrollback via un
                  compteur d'éviction ajouté au fork `charmbracelet/x/vt`, 0009: watchers de motifs
                  par session — anti-rebond par motif, tap partagé avec la détection d'agent,
                  0010: redémarrage automatique des sessions, 0011: le pass-through devient l'état
                  par défaut — étend les ADR 0004/0005, 0012: verrouillage par session — amende la
                  décision 1 de l'ADR 0011, `locked:` dans le fichier projet)
  repports/       (sic) historical analysis reports
site/             sources of the bilingual GitHub Pages site (site/en/, site/fr/)
```

## Documentation language policy

- **`README.md` is English-only** and is the reference version: every user-facing doc change lands
  there first. It is checked by `pkg/config/doc_test.go`, which parses its `### Reference` table and
  `### Example` YAML block against the `Config` struct — those two headings and the table/YAML
  formats are load-bearing, and adding a config field without documenting it fails the build.
- **`docs/README.fr.md` is its French translation**, kept in sync by hand. The two cross-link each
  other at the top. Its config table and example mirror the English ones (same keys, same defaults);
  nothing parses it, so keep it faithful rather than clever.
- **`site/`** carries the same material as a bilingual site (`site/en/`, `site/fr/`) published on
  GitHub Pages. A user-facing change worth documenting usually touches all three.
- ADRs (`docs/adr/`) and the historical reports (`docs/repports/`) are French, and stay French —
  they are records of decisions taken, not living documentation.
- The application itself ships both languages (`pkg/i18n`, `language:` config); its CLI output
  (`lazyshell config ...`) stays French.

## Open items

Each entry carries its own rationale — this list is the reference for what was decided and why,
now that the roadmap that used to hold it is gone.


- Mouse support: **done (phase 12, ADR 0003)**, on by default. gocui's mouse/Shift-arrow collision
  turned out to cover only two values, and the cost paid is that `Shift-Up`/`Shift-Down` are no
  longer forwarded to a session while `mouse.enabled` is true. The load-bearing rule: mouse events
  are dropped at the top of `editOutput` before `keys.Translate`, or a click gets typed into the
  shell as `\x1b[1;2B`. The wheel scrolls the panel's content and is *never* encoded as an arrow
  key; it only reaches the session's program once that program arms a DECSET 9/1000/1002/1003.
- Windows support: explicitly out of scope (no Unix pty).
- Agent control API (agents creating panels / reading other sessions' output via a socket): **done
  (ADR 0006)**, and the decision it reverses was explicit, so read that ADR before touching any of
  it. `pkg/control` + `lazyshell ctl`, nine verbs (`list`/`read`/`new`/`send`/`kill`/`rename` plus
  ADR 0007's `group`/`group-send`/`group-kill`), on a
  *separate* socket with a *separate* protocol (line-JSON, request→response) from the phase 11b hook
  channel — which stays inbound/declarative and open by default precisely because all it can do is
  move a marker. The load-bearing rules: `control.enabled` is false by default and, when false,
  there is no socket at all and no `$LAZYSHELL_CONTROL_SOCK` in any session's environment (its
  absence is the signal); there is no token, so enabling it means every process running as the user
  can drive lazyshell; `ctl` exits non-zero on failure, the exact opposite of `lazyshell hook`; and
  the goroutine split in `pkg/gui/control.go` (`list`/`read`/`send`/`group-send` inline,
  `new`/`rename`/`group` through `onGUI`, `kill`/`group-kill` split between the two) is a
  correctness rule, not a style choice.
- Session groups: **done (ADR 0007)**. One group per session, a *display* property — `Manager.order`
  stays the creation order and the grouping is recomputed every tick. The load-bearing rule, and the
  one to read the ADR before touching: `gui.selectedIndex` indexes **sessions** in display order,
  never view lines. Group headers are unselectable rows, and `rowLineForSessionIndex` is what makes
  it impossible for gocui's `Highlight` to ever paint one — the guarantee is the function's return
  type, not a check anywhere. Only `pkg/gui/sessions_panel.go`'s renderer and `clickSession` may
  convert between the two spaces. A project file may declare group *names and their order* and
  nothing else, per `ProjectConfig`'s whitelist doctrine. Ctrl keys are unusable for any remappable
  action until `keyLabel`'s output round-trips through `gocui.Parse` (see the ADR's decision 6) —
  which is why the group keys are `g`/`G`/`A`/`X`/`W`.
- Per-session lock state: **done (ADR 0012)**, amending decision 1 of ADR 0011. `passThroughActive`
  is still the single flag everything reads, but `Gui.lockedBySession` (explicit entries only, keyed
  by session id) remembers what was decided per session, and `onSelectionChanged` applies it. A
  project file's `locked:` seeds it through `SetLockedSessions`, defaulting to "a declared
  `command:` starts locked". The load-bearing rules: a session with *no* entry keeps ADR 0011's
  persistence (the flag carries over untouched); only a user gesture is remembered, so technical
  locks (`setTab` leaving the terminal tab, `backOutOfExitedSession`) must call `lockOutput` and
  never `exitPassThrough`; and `enterPassThrough` records *before* calling `onSelectionChanged`,
  which would otherwise re-lock what it just unlocked.
- Detach/daemon mode ("agents keep running with the laptop closed"): out of scope unless real demand
  surfaces.
