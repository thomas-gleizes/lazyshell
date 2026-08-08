# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

`lazyshell` is implemented and functional, past v1.1. Everything the phased roadmap planned, through
phase 13 ("onglets du panneau de sortie"), is built. This is an active Go codebase with a real
`go.mod`, CI, goreleaser packaging, and a test suite — do not treat this as a design-stage repo.

`ROADMAP.md` no longer exists (removed in `04e4d9c`): the phase-by-phase history it carried is in
the git log, and what it said about the architecture lives in the two sections below. Code comments
and ADRs still cite phase numbers ("phase 11b", "the phase-1 spike") — those are historical
coordinates into that history, not a pointer to a file you can open.

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
  keys/           keybinding definitions
  i18n/           strings/translations
  version/        --version metadata (goreleaser-injected)
docs/
  README.fr.md    French translation of the root README
  adr/            architecture decision records (0001: rendu ANSI et clavier, 0002: rendu
                  multi-panneaux, 0003: souris, 0004: sortie du pass-through — remplace la
                  décision 3 de l'ADR 0001)
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
- Agent control API (agents creating panels / reading other sessions' output via a socket): decided
  against for now — the phase 11b hooks socket is inbound/declarative only; an outbound control verb
  is a deliberately separate, not-yet-taken decision (untrusted process, execution surface risk).
- Detach/daemon mode ("agents keep running with the laptop closed"): out of scope unless real demand
  surfaces.
