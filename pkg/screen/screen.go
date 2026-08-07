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
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// Screen is a terminal emulator safe for concurrent use: the session's drain
// goroutine writes into it while the render loop reads from it.
type Screen struct {
	mu   sync.Mutex
	term *vt.Emulator

	// The four fields below mirror emulator state that vt keeps but does not
	// expose a getter for; they are maintained by the callbacks installed in
	// NewWithScrollback.
	//
	// Those callbacks fire from inside term.Write — that is, with s.mu already
	// held by Write — so they may write these fields directly, and the
	// accessors below read them under the same mu. The rule that comes with
	// it: a callback handler must never call back into a Screen method, or it
	// would deadlock on a mutex it is already holding.
	cursorVisible   bool
	appCursorKeys   bool
	title           string
	bellPending     bool
	activityPending bool
}

// New returns a Screen of the given size, in cells, with the emulator's
// default scrollback size (vt.DefaultScrollbackSize).
func New(cols, rows int) *Screen {
	return NewWithScrollback(cols, rows, vt.DefaultScrollbackSize)
}

// NewWithScrollback is New with an explicit scrollback size, in lines —
// pkg/config's ScrollbackSize, threaded through by pkg/session.Manager.
func NewWithScrollback(cols, rows, scrollback int) *Screen {
	term := vt.NewEmulator(cols, rows)
	term.SetScrollbackSize(scrollback)

	// The cursor starts visible (vt.Cursor's zero value is Hidden: false) and
	// CursorVisibility only fires on a change, so the initial value has to be
	// seeded here rather than inferred from the first callback.
	s := &Screen{term: term, cursorVisible: true}

	term.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { s.cursorVisible = visible },
		Title:            func(title string) { s.title = title },
		Bell:             func() { s.bellPending = true },
		EnableMode:       func(mode ansi.Mode) { s.setMode(mode, true) },
		DisableMode:      func(mode ansi.Mode) { s.setMode(mode, false) },
	})

	return s
}

// setMode records the emulator modes lazyshell cares about. Called from vt's
// EnableMode/DisableMode callbacks, so s.mu is already held — see the field
// comments on Screen.
func (s *Screen) setMode(mode ansi.Mode, set bool) {
	if mode == ansi.ModeCursorKeys {
		s.appCursorKeys = set
	}
}

// Write feeds pty output into the emulator.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, err := s.term.Write(p)
	if n > 0 {
		s.activityPending = true
	}

	return n, err
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
// highlight, if non-empty, marks every case-insensitive occurrence of that
// substring in reverse video — the scrollback-search feature's rendering
// half (Find is the other half, in search.go). Passing "" skips this
// entirely and, combined with offset <= 0, keeps the original fast path:
// the emulator's own Render, with no line-by-line reconstruction.
//
// The emulator has no built-in scroll viewport — Render always shows the
// live screen — so any other case rebuilds the requested window itself via
// linesAt: scrolled-off rows come from Scrollback().Line, still-live rows
// are reconstructed cell by cell via CellAt (bounded by the panel's height,
// so this stays cheap), and the combined slice is rendered with
// uv.Lines.Render, the same styling path vt.Emulator.Render uses internally.
func (s *Screen) RenderAt(offset int, highlight string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if offset <= 0 && highlight == "" {
		return s.term.Render()
	}

	lines := s.linesAt(offset)

	if highlight != "" {
		lower := strings.ToLower(highlight)
		for i, line := range lines {
			lines[i] = highlightLine(line, lower)
		}
	}

	return lines.Render()
}

// windowStart returns the absolute index (Find's contract: the index i such
// that offset = ScrollbackLen() - i puts line i at the top of the window) of
// the first row offset lines back from the live bottom, and ScrollbackLen()
// alongside it since every caller needs both. offset is clamped to
// [0, ScrollbackLen()], the same clamp RenderAt documents. Callers must hold
// s.mu.
func (s *Screen) windowStart(offset int) (start, scrollbackLen int) {
	scrollbackLen = s.term.Scrollback().Len()

	if offset < 0 {
		offset = 0
	} else if offset > scrollbackLen {
		offset = scrollbackLen
	}

	return scrollbackLen - offset, scrollbackLen
}

// linesAt builds the raw uv.Lines window offset lines back from the live
// bottom, offset <= 0 meaning "all rows live" — the same window RenderAt's
// fast path renders directly via the emulator when there is no highlight to
// apply. Callers must hold s.mu.
func (s *Screen) linesAt(offset int) uv.Lines {
	scrollback := s.term.Scrollback()
	start, scrollbackLen := s.windowStart(offset)

	rows := s.term.Height()
	cols := s.term.Width()

	lines := make(uv.Lines, 0, rows)
	for i := range rows {
		idx := start + i
		if idx < scrollbackLen {
			lines = append(lines, scrollback.Line(idx))

			continue
		}

		lines = append(lines, s.liveLine(idx-scrollbackLen, cols))
	}

	return lines
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

// CursorPosition is where the emulated cursor sits, in cells, relative to the
// top-left of the visible screen. Meaningful for display only while the live
// screen is shown: RenderAt with a non-zero offset shows history the cursor is
// not in.
func (s *Screen) CursorPosition() (x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pos := s.term.CursorPosition()

	return pos.X, pos.Y
}

// CursorVisible reports whether the application wants the cursor drawn — full
// screen applications hide it while they redraw (DECTCEM, ESC[?25l). vt tracks
// this internally but exposes no getter, hence the callback in
// NewWithScrollback.
func (s *Screen) CursorVisible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cursorVisible
}

// ApplicationCursorKeys reports whether DECCKM is set. When it is, the arrow
// and Home/End keys must be sent as SS3 sequences (ESC O A) rather than CSI
// ones (ESC [ A) — less and several full-screen applications only accept the
// former. Consumed by pkg/keys.TranslateWithMode.
func (s *Screen) ApplicationCursorKeys() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.appCursorKeys
}

// Title is the terminal title the application last set through OSC 0/2 —
// typically the running command, which is exactly what a session manager wants
// to list. Empty when nothing has set one.
func (s *Screen) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.title
}

// BellPending reports whether a BEL has been received since the last
// ClearBell. It is a latch, not an event: a session that rang while it was not
// on screen must still be able to say so when the user comes back to it.
func (s *Screen) BellPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.bellPending
}

// ClearBell acknowledges a pending bell.
func (s *Screen) ClearBell() {
	s.mu.Lock()
	s.bellPending = false
	s.mu.Unlock()
}

// ActivityPending reports whether output has been received since the last
// ClearActivity. Like BellPending, a latch rather than an event: a session
// that produced output while it was not on screen must still be able to say
// so when the user comes back to the list.
func (s *Screen) ActivityPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.activityPending
}

// ClearActivity acknowledges pending activity.
func (s *Screen) ClearActivity() {
	s.mu.Lock()
	s.activityPending = false
	s.mu.Unlock()
}
