// Command spike-pty is the phase 1 throwaway spike of ROADMAP.md: a single
// gocui view, a single shell behind a pty, to answer the only question with no
// precedent in lazygit/lazydocker — can gocui and an interactive pty share the
// terminal?
//
// It is deliberately not wired into pkg/gui: what it validates is the
// translation table (pkg/keys), the resize propagation and the ANSI rendering
// limits. The conclusions live in docs/adr/0001-rendu-ansi-et-clavier.md.
//
// Usage: go run ./cmd/spike-pty   —   Ctrl-B q to quit, Ctrl-B Ctrl-B for a
// literal Ctrl-B.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/ansi"
	"github.com/thomas-gleizes/lazyshell/pkg/keys"
)

const (
	viewName = "session"

	// defaultPrefixKey is the tmux-style escape prefix. Every other key goes to
	// the shell, so this is the only way back to lazyshell itself. It is
	// overridable through LAZYSHELL_PREFIX, because a terminal multiplexer
	// running above us may well eat Ctrl-B before we ever see it.
	defaultPrefixKey = gocui.KeyCtrlB

	reRenderInterval = 30 * time.Millisecond

	// A gocui view keeps everything ever written to it, and the cost of a
	// redraw grows linearly with that buffer: measured at 11ms for 12k lines
	// and 49ms for 72k lines. Redrawing every 30ms, a chatty session (htop
	// redrawing in place) saturates the main loop and the UI stops answering
	// keypresses. Phase 2 replaces this with a proper ring buffer.
	maxBufferLines  = 5000
	keepBufferLines = 4000
)

type spike struct {
	g    *gocui.Gui
	ptmx *os.File

	// prefixKey is the escape prefix in force, see defaultPrefixKey.
	prefixKey gocui.Key

	// prefixPending is true between the prefix key and the key it qualifies.
	prefixPending bool

	// quitRequested records that the escape sequence fired, so the behaviour
	// is observable without running a main loop.
	quitRequested bool

	// lastEvent describes the last key event received, shown in the view
	// subtitle: the spike exists to find out what the terminal actually sends.
	lastEvent string

	// lastRows/lastCols avoid sending a SIGWINCH to the shell on every single
	// redraw: layout runs dozens of times per second.
	mu       sync.Mutex
	lastRows int
	lastCols int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spike-pty: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	// OutputTrue is required, not cosmetic: in OutputNormal gocui only parses
	// the 8-colour SGR forms and prints "[38;5;2m" as literal text, which is
	// what any themed shell prompt emits.
	g, err := gocui.NewGui(gocui.NewGuiOpts{OutputMode: gocui.OutputTrue})
	if err != nil {
		return fmt.Errorf("failed to initialise the terminal: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n\n%s", r, debug.Stack())
		}
	}()
	defer g.Close()

	g.Cursor = false
	// The mouse must stay off: gocui reports the mouse buttons with the same
	// key values as Shift-arrows (see pkg/keys).
	g.Mouse = false
	g.InputEsc = true

	s := &spike{g: g, prefixKey: prefixFromEnv()}

	cmd := exec.Command(shell())
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start %s behind a pty: %w", shell(), err)
	}
	defer func() { _ = ptmx.Close() }()
	s.ptmx = ptmx

	g.SetManagerFunc(s.layout)

	// The shell owns the terminal input while it is displayed: no keybinding
	// is registered, everything goes through the editor.

	go s.drain(cmd)
	s.goEvery(reRenderInterval, s.reRender)

	if err := g.MainLoop(); err != nil && !goerrors.Is(err, gocui.ErrQuit) {
		return err
	}

	// Do not leave the shell (and its children) behind.
	_ = cmd.Process.Kill()

	return nil
}

func shell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}

	return "/bin/bash"
}

// drain copies the pty output into the view. gocui.View.Write is thread-safe,
// so no g.Update is needed here; the redraw is triggered by the ticker.
func (s *spike) drain(cmd *exec.Cmd) {
	view, err := s.waitForView()
	if err != nil {
		return
	}

	_, _ = io.Copy(ansi.NewWriter(view), s.ptmx)

	// The pty reached EOF: the shell is gone (exit, Ctrl-D...).
	_ = cmd.Wait()
	s.quit()
}

// waitForView polls until the layout has created the view, which happens on the
// first redraw — the drain goroutine starts before that.
func (s *spike) waitForView() (*gocui.View, error) {
	for range 100 {
		if view, err := s.g.View(viewName); err == nil {
			return view, nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return nil, fmt.Errorf("view %q never appeared", viewName)
}

func (s *spike) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	if maxX < 2 || maxY < 2 {
		return nil
	}

	view, err := g.SetView(viewName, 0, 0, maxX-1, maxY-1, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = fmt.Sprintf(" spike-pty — %s puis q pour quitter ", prefixName(s.prefixKey))
		view.Wrap = true
		view.Autoscroll = true
		view.Editable = true
		view.Editor = gocui.EditorFunc(s.edit)

		if _, err := g.SetCurrentView(viewName); err != nil {
			return err
		}
	}

	return s.resize(view)
}

// resize propagates the view's inner size to the pty, so the shell wraps its
// lines where the panel actually ends.
func (s *spike) resize(view *gocui.View) error {
	cols, rows := view.Size()
	if cols <= 0 || rows <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if cols == s.lastCols && rows == s.lastRows {
		return nil
	}

	if err := pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}); err != nil {
		return fmt.Errorf("pty.Setsize: %w", err)
	}

	s.lastCols, s.lastRows = cols, rows

	return nil
}

// edit receives every keypress of the focused view and forwards it to the
// shell, except the escape prefix and the key that follows it.
func (s *spike) edit(_ *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	s.lastEvent = fmt.Sprintf("key=%d ch=%q mod=%d", key, ch, mod)

	if s.prefixPending {
		s.prefixPending = false

		switch {
		case ch == 'q':
			s.quit()
		case key == s.prefixKey:
			// Doubled prefix: send it literally.
			s.write(keys.Translate(key, ch, mod))
		}

		return true
	}

	if key == s.prefixKey && mod == gocui.ModNone {
		s.prefixPending = true

		return true
	}

	s.write(keys.Translate(key, ch, mod))

	return true
}

func (s *spike) write(b []byte) {
	if len(b) == 0 {
		return
	}

	_, _ = s.ptmx.Write(b)
}

func (s *spike) quit() {
	s.quitRequested = true

	s.g.Update(func(*gocui.Gui) error { return gocui.ErrQuit })
}

// reRender pushes the view to the screen when the drain goroutine has written
// to it. Writing to a view only marks it tainted; the redraw is on us.
func (s *spike) reRender() error {
	view, err := s.g.View(viewName)
	if err != nil {
		return nil
	}

	s.g.Update(func(*gocui.Gui) error {
		s.updateSubtitle(view)

		return nil
	})

	if view.IsTainted() {
		// Trimming happens inside Update so it runs on the main loop, like
		// every other mutation of gocui state.
		s.g.Update(func(*gocui.Gui) error {
			trim(view)

			return nil
		})
	}

	return nil
}

// updateSubtitle shows the last key event and whether the prefix is armed.
// Without it there is no way to tell "the key never arrived" from "the key
// arrived and was mishandled".
func (s *spike) updateSubtitle(view *gocui.View) {
	state := ""
	if s.prefixPending {
		state = " [PREFIXE ARME]"
	}

	view.Subtitle = fmt.Sprintf(" %s%s ", s.lastEvent, state)
}

// prefixFromEnv reads LAZYSHELL_PREFIX, in gocui's keybinding syntax:
// "Ctrl+A", "Ctrl+G", "Ctrl+Space"... An unparseable value falls back to the
// default rather than leaving the user with no way out.
func prefixFromEnv() gocui.Key {
	name := os.Getenv("LAZYSHELL_PREFIX")
	if name == "" {
		return defaultPrefixKey
	}

	key, _, err := gocui.Parse(name)
	if err != nil {
		return defaultPrefixKey
	}

	if k, ok := key.(gocui.Key); ok {
		return k
	}

	return defaultPrefixKey
}

func prefixName(key gocui.Key) string {
	if key >= gocui.KeyCtrlA && key <= gocui.KeyCtrlZ {
		return fmt.Sprintf("Ctrl-%c", 'A'+(key-gocui.KeyCtrlA))
	}

	return fmt.Sprintf("touche %d", key)
}

// trim caps the view buffer so the redraw cost stays bounded.
func trim(view *gocui.View) {
	if view.LinesHeight() <= maxBufferLines {
		return
	}

	lines := view.BufferLines()
	view.SetContent(strings.Join(lines[len(lines)-keepBufferLines:], "\n"))
}

func (s *spike) goEvery(interval time.Duration, fn func() error) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			_ = fn()
		}
	}()
}
