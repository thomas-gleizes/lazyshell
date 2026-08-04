# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

`lazyshell` is pre-implementation. This repository currently contains **only a design/research
document** (`RAPPORT_ANALYSE_LAZYGIT_LAZYDOCKER.md`, in French) — there is no source code, no Go
module, no build tooling yet. When asked to "build the project" or similar, start from the
architecture proposal in that document rather than assuming any existing scaffolding.

## Project goal

`lazyshell` is meant to be a **TUI shell session manager** (a tmux/screen-like multiplexer), built
in Go on top of `gocui`. It should let a user manage multiple persistent shell sessions from a
`lazygit`/`lazydocker`-style two-pane interface: a session list on the left, live session output on
the right.

## Key architectural decisions already made (see the report for full rationale)

The report analyzed `lazygit` and `lazydocker` (both built on `jesseduffield/gocui`) to extract
reusable patterns. Conclusions to follow when implementing:

- **Base library**: `github.com/jesseduffield/gocui`. A `gocui.View` is an `io.Writer` and is
  thread-safe to write to from any goroutine (internal `writeMutex`), but any other gocui state
  mutation (current view, dimensions) must go through `g.Update(...)` to stay on the main loop
  goroutine.
- **Layout engine**: reuse `github.com/jesseduffield/lazycore/pkg/boxlayout` (weighted row/column
  tree) for the sessions-list / output-panel split, including automatic portrait-mode stacking on
  narrow terminals.
- **Keybindings**: start with lazydocker's flat `Binding{ViewName, Key, Modifier, Handler}` +
  `g.SetKeybinding` model. Do **not** adopt lazygit's heavier "controller" pattern
  (`GetKeybindings()` per domain, context stack) unless the action set grows large enough to need
  it — it's overkill for an MVP.
- **Async task management**: port lazydocker's `TaskManager` (`pkg/tasks/tasks.go`) pattern —
  one task = one goroutine + cancelable `context.Context`; starting a new task auto-stops the
  previous one. Important deviation from lazydocker: this must only cancel the *reader/renderer*
  goroutine when switching session selection, never the underlying shell process — shells must
  keep running in the background even when not the currently displayed session (unlike Docker
  container logs, which can be re-streamed on demand).
- **PTY handling (new — absent from both lazygit and lazydocker)**: use `github.com/creack/pty`.
  Neither reference project needs a real pty (Docker logs are output-only; interactive commands
  just get `Suspend()`/`Resume()` of the whole terminal). A session manager needs one pty per
  session, `pty.Setsize` propagated from the panel's computed layout size on resize, keystrokes
  routed to `ptmx.Write()` in "pass-through" mode when the output panel has focus, and an
  explicit scrollback buffer per session (a pty does not replay history the way `docker logs
  --since` does).
- **Process lifecycle**: unlike Docker (daemon owns container lifecycle independently of
  lazydocker), `lazyshell` itself is the parent process that must keep shells alive across
  selection changes — plan on a `map[sessionID]*Session{cmd, ptmx, scrollback, cancel}` held in
  app state, decoupled from the `TaskManager` (which should only own display/reading goroutines).

## Proposed package layout (from the report, not yet created)

```
pkg/
  app/            bootstrap: load config, build SessionManager, run gui.Run()
  session/        SessionManager: CRUD (New, Kill, List); Session{cmd, ptmx, scrollback, status}
  gui/
    gui.go        gocui init, MainLoop, goEvery(reRenderOutput), keybindings
    layout.go     boxlayout: "sessions" panel (left, fixed width) + "output" panel (right)
    sessions_panel.go  session list, OnSelect -> QueueTask(stream)
    output.go     QueueTask doing io.Copy(outputView, session.PtyReader) via TaskManager
    input.go      keystroke capture when output panel focused -> ptmx.Write(bytes)
    keybindings.go  n: new session, x/d: kill session, tab: cycle focus
    theme.go / config/  YAML config + theme, modeled on lazydocker's approach
  tasks/          near-verbatim port of lazydocker's pkg/tasks (TaskManager, NewTickerTask)
```

Typical flow: selecting a session in the list triggers `OnSelect`, which queues a task that
`io.Copy`s from the session's live reader into the output view (the TaskManager auto-kills the
previous stream); keystrokes while the output panel is focused write directly to `session.ptmx`;
a separate goroutine — independent of the TaskManager, started at session creation and living for
the life of the process — continuously drains the pty into the session's scrollback buffer, so no
output is lost while a session is not on screen.
