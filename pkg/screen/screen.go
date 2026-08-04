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
func (s *Screen) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.term.Read(p)
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
