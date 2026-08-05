// Package screen wraps a terminal emulator: the pty writes into it, and it
// renders the resulting screen for display.
//
// This replaces the earlier "filter the escape sequences gocui cannot render"
// approach, which could not work: a themed shell prompt redraws itself in place
// (cursor up, erase, rewrite), and an append-only view stacks every redraw
// instead of overwriting it. Measured on a real zsh startup: 5 ESC[A, 4 ESC[J
// and 16 CR before the user types anything, producing four stacked prompts.
// See docs/adr/0001-rendu-ansi-et-clavier.md.
package screen

import (
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// Screen is a terminal emulator safe for concurrent use: the session's drain
// goroutine writes into it while the render loop reads from it.
type Screen struct {
	mu   sync.Mutex
	term *vt.Emulator
}

// New returns a Screen of the given size, in cells.
func New(cols, rows int) *Screen {
	return &Screen{term: vt.NewEmulator(cols, rows)}
}

// Write feeds pty output into the emulator.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.term.Write(p)
}

// Read returns what the emulator answers back to the application: terminal
// capability queries, cursor position reports... These must be written to the
// pty, otherwise a shell that asks a question waits for an answer that never
// comes — and the stray bytes end up displayed instead.
//
// Deliberately not guarded by mu: the underlying read blocks on an internal
// pipe until there is something to answer, which is most of the time (replies
// only happen for DA/CPR/OSC-colour/focus/mouse queries). Holding the lock for
// that unbounded wait would block every Write and Render call for as long as
// the session runs, and Write itself sometimes writes a reply synchronously
// into the same pipe — a caller looping on Read while holding the lock would
// deadlock its own Write. This mirrors vt.SafeEmulator.Read, the only method
// its own concurrency wrapper also leaves unlocked, for the same reason.
func (s *Screen) Read(p []byte) (int, error) {
	return s.term.Read(p)
}

// Close shuts the emulator down, which unblocks any pending Read with io.EOF.
// It is the only way to release a goroutine parked in Read once the session is
// done: closing the pty's file descriptor does not affect this in-memory pipe.
func (s *Screen) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.term.Close()
}

// Render returns the visible screen, with SGR sequences for colours and
// attributes. Its size is bounded by the terminal geometry, so the cost of a
// redraw no longer grows with the amount of output — which is what used to
// freeze the UI on a chatty session.
func (s *Screen) Render() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.term.Render()
}

// RenderAt returns the screen as it looked offset lines back from the live
// bottom, in the same SGR-encoded shape as Render. offset <= 0 is the live
// view (identical to Render); offset is clamped to ScrollbackLen(), so
// scrolling past the oldest history just stops there instead of erroring.
//
// The emulator has no built-in scroll viewport — Render always shows the
// live screen — so this rebuilds the requested window itself: scrolled-off
// rows come from Scrollback().Line, still-live rows are reconstructed cell
// by cell via CellAt (bounded by the panel's height, so this stays cheap),
// and the combined slice is rendered with uv.Lines.Render, the same styling
// path vt.Emulator.Render uses internally.
func (s *Screen) RenderAt(offset int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if offset <= 0 {
		return s.term.Render()
	}

	scrollback := s.term.Scrollback()
	scrollbackLen := scrollback.Len()
	if offset > scrollbackLen {
		offset = scrollbackLen
	}

	rows := s.term.Height()
	cols := s.term.Width()
	start := scrollbackLen - offset

	lines := make(uv.Lines, 0, rows)
	for i := range rows {
		idx := start + i
		if idx < scrollbackLen {
			lines = append(lines, scrollback.Line(idx))

			continue
		}

		lines = append(lines, s.liveLine(idx-scrollbackLen, cols))
	}

	return lines.Render()
}

// liveLine reconstructs row y of the still-live screen as a uv.Line, cell by
// cell: the emulator exposes CellAt but not a way to fetch a whole live row
// at once. Callers must hold s.mu.
func (s *Screen) liveLine(y, cols int) uv.Line {
	line := make(uv.Line, cols)
	for x := range cols {
		if c := s.term.CellAt(x, y); c != nil {
			line[x] = *c
		}
	}

	return line
}

// Resize changes the emulated geometry. The caller is responsible for calling
// pty.Setsize as well, so the shell learns about it too.
func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.term.Resize(cols, rows)
}

// IsAltScreen reports whether a full-screen application (vim, htop, less) is
// currently in control.
func (s *Screen) IsAltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.term.IsAltScreen()
}

// ScrollbackLen is the number of lines that have scrolled off the top.
func (s *Screen) ScrollbackLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.term.ScrollbackLen()
}
