// Package debug is the --debug mode's recorder: a small append-only log file
// plus an in-memory ring of the most recent entries, which pkg/gui draws in a
// floating panel over the output panel.
//
// It exists because once gocui owns the terminal there is nowhere left to
// print: stderr is unusable and the status bar is one line. When a key does
// not do what it should — a combination gocui swallowed, a remap that did not
// take, a Shift-Down turned into a click by the mouse collision of ADR 0003 —
// this is what there is to look at.
//
// The central rule of the whole package: **a nil *Logger is the "debug mode
// off" state**, and every method is nil-safe. Call sites therefore write
// gui.debug.Key(...) unconditionally, the same way pkg/i18n's Catalog.T is
// nil-safe, which is what makes it possible to sprinkle calls through the
// input path without wrapping each one in an if.
package debug

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ringSize is how many entries the in-memory ring keeps for the panel. The
// panel shows at most a couple of dozen lines; the rest of the margin is so a
// burst of keystrokes does not scroll away what caused it before it can be
// read. Everything is in the file anyway — the ring is only the live view.
const ringSize = 300

// timeFormat is what the file gets: a full date, because the file is appended
// to across runs. The panel renders its own, shorter form.
const timeFormat = "2006-01-02 15:04:05.000"

// Kind is what a line is about. The three are deliberately coarse: reading a
// debug log is scanning for one of them, not filtering a taxonomy.
type Kind int

const (
	// KindKey is a raw or normalized keystroke seen by the output panel's
	// Editor.
	KindKey Kind = iota
	// KindAction is an action that actually fired — a registered keybinding,
	// a mouse gesture, or a branch of the Editor's own switch.
	KindAction
	// KindEvent is everything else worth a timestamp: session lifecycle,
	// agent state transitions, resizes, tab changes.
	KindEvent
)

// String is the tag written to the file. Fixed width so the file stays
// column-aligned and greppable.
func (k Kind) String() string {
	switch k {
	case KindKey:
		return "KEY"
	case KindAction:
		return "ACT"
	case KindEvent:
		return "EVT"
	default:
		return "???"
	}
}

// Entry is one recorded line, as the panel consumes it.
type Entry struct {
	At   time.Time
	Kind Kind
	Text string
}

// Logger writes to the file and keeps the last ringSize entries in memory.
//
// mu is not optional: Key and Action come from gocui's own goroutine, but
// Event is called from the goEvery background tickers (session exits, agent
// state, sampling), and Recent is read from whichever goroutine is drawing.
type Logger struct {
	mu   sync.Mutex
	f    *os.File
	path string
	// ring is a fixed-size circular buffer; next is where the following
	// entry goes. count saturates at len(ring) and is what tells Recent
	// whether the buffer has wrapped yet.
	ring  []Entry
	next  int
	count int
}

// New opens (creating it if needed) the log file at path and writes a header
// line marking the start of this run. The file is appended to, never
// truncated: comparing two runs is most of the point.
//
// 0o600 rather than 0o644 because the log contains every keystroke typed into
// a shell — including whatever was typed at a password prompt of a program
// that does not disable echo.
func New(path string) (*Logger, error) {
	if path == "" {
		return nil, fmt.Errorf("debug: no log file path (unknown home directory?)")
	}

	// The config directory does not necessarily exist yet: nothing creates it
	// until `lazyshell config init` is run, and --debug must work on a machine
	// where it never has been. Same 0o700 as trust.go's store, for the same
	// reason — what goes in here is nobody else's business.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("debug: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("debug: %w", err)
	}

	return &Logger{f: f, path: path, ring: make([]Entry, ringSize)}, nil
}

// Path is where New opened the file, so the caller can tell the user where to
// look. Empty on a nil Logger.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}

	return l.path
}

// Key, Action and Event record one line of their respective kind. All three
// are no-ops on a nil Logger — see the package comment.
func (l *Logger) Key(format string, args ...any) { l.log(KindKey, format, args...) }

func (l *Logger) Action(format string, args ...any) { l.log(KindAction, format, args...) }

func (l *Logger) Event(format string, args ...any) { l.log(KindEvent, format, args...) }

// log appends to the ring and to the file. Deliberately unbuffered: the
// volume is a few lines per keystroke, and the run that most needs a debug log
// is the one that ends in a crash, which would take a buffer's tail with it.
func (l *Logger) log(kind Kind, format string, args ...any) {
	if l == nil {
		return
	}

	entry := Entry{At: time.Now(), Kind: kind, Text: fmt.Sprintf(format, args...)}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.ring[l.next] = entry
	l.next = (l.next + 1) % len(l.ring)

	if l.count < len(l.ring) {
		l.count++
	}

	if l.f != nil {
		fmt.Fprintf(l.f, "%s %s %s\n", entry.At.Format(timeFormat), kind, entry.Text)
	}
}

// Recent returns up to n entries, oldest first, ending with the most recent
// one — the order a panel that scrolls downwards wants. n <= 0, or a nil
// Logger, yields nil.
func (l *Logger) Recent(n int) []Entry {
	if l == nil || n <= 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if n > l.count {
		n = l.count
	}

	out := make([]Entry, 0, n)
	// The oldest of the n wanted entries sits n slots behind next, modulo the
	// ring — the +len(l.ring) keeps that index non-negative before the wrap.
	start := (l.next - n + len(l.ring)) % len(l.ring)

	for i := 0; i < n; i++ {
		out = append(out, l.ring[(start+i)%len(l.ring)])
	}

	return out
}

// Close flushes nothing (writes are unbuffered) and releases the file. Safe on
// a nil Logger so pkg/app can defer it unconditionally.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.f == nil {
		return nil
	}

	err := l.f.Close()
	l.f = nil

	return err
}
