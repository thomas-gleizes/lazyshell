package debug

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestLogger opens a logger in a temp directory and closes it for the test.
func newTestLogger(t *testing.T) (*Logger, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "debug.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = l.Close() })

	return l, path
}

// A nil *Logger is the "debug mode off" state and every method has to survive
// it: this is the whole reason call sites can write gui.debug.Key(...) with no
// guard around it.
func TestNilLoggerIsANoOp(t *testing.T) {
	var l *Logger

	l.Key("k")
	l.Action("a")
	l.Event("e")

	if got := l.Recent(10); got != nil {
		t.Errorf("Recent on nil logger = %v, want nil", got)
	}

	if got := l.Path(); got != "" {
		t.Errorf("Path on nil logger = %q, want empty", got)
	}

	if err := l.Close(); err != nil {
		t.Errorf("Close on nil logger = %v, want nil", err)
	}
}

// Nothing creates ~/.config/lazyshell until `config init` is run, so --debug
// has to be able to make the directory itself.
func TestNewCreatesTheParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazyshell", "debug.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New into a missing directory: %v", err)
	}

	t.Cleanup(func() { _ = l.Close() })

	if _, err := os.Stat(path); err != nil {
		t.Errorf("log file not created: %v", err)
	}
}

func TestNewRejectsEmptyPath(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("New(\"\") = nil error, want one")
	}
}

func TestLogWritesToFile(t *testing.T) {
	l, path := newTestLogger(t)

	l.Key("ctrl-o")
	l.Action("new_session")
	l.Event("session-1 started")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), string(data))
	}

	for i, want := range []string{"KEY ctrl-o", "ACT new_session", "EVT session-1 started"} {
		if !strings.HasSuffix(lines[i], want) {
			t.Errorf("line %d = %q, want it to end with %q", i, lines[i], want)
		}
	}
}

// The file is appended to, never truncated: comparing two runs is most of the
// point of having a file at all.
func TestNewAppendsToAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")

	first, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first.Event("run one")

	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := New(path)
	if err != nil {
		t.Fatalf("New (second run): %v", err)
	}

	second.Event("run two")
	_ = second.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !strings.Contains(string(data), "run one") || !strings.Contains(string(data), "run two") {
		t.Errorf("second run truncated the file: %q", string(data))
	}
}

func TestRecentReturnsOldestFirst(t *testing.T) {
	l, _ := newTestLogger(t)

	l.Event("one")
	l.Event("two")
	l.Event("three")

	got := l.Recent(10)
	if len(got) != 3 {
		t.Fatalf("Recent(10) returned %d entries, want 3", len(got))
	}

	for i, want := range []string{"one", "two", "three"} {
		if got[i].Text != want {
			t.Errorf("entry %d = %q, want %q", i, got[i].Text, want)
		}
	}
}

func TestRecentClampsToWhatIsAsked(t *testing.T) {
	l, _ := newTestLogger(t)

	for _, text := range []string{"one", "two", "three"} {
		l.Event("%s", text)
	}

	got := l.Recent(2)
	if len(got) != 2 {
		t.Fatalf("Recent(2) returned %d entries, want 2", len(got))
	}

	// The two most recent ones, still oldest first.
	if got[0].Text != "two" || got[1].Text != "three" {
		t.Errorf("Recent(2) = %q/%q, want two/three", got[0].Text, got[1].Text)
	}

	if got := l.Recent(0); got != nil {
		t.Errorf("Recent(0) = %v, want nil", got)
	}
}

// Past ringSize the buffer wraps, and Recent must keep reading it in order
// rather than restarting at the physical start of the slice.
func TestRingEvictsOldestEntries(t *testing.T) {
	l, _ := newTestLogger(t)

	for i := 0; i < ringSize+5; i++ {
		l.Event("entry-%d", i)
	}

	got := l.Recent(ringSize + 100)
	if len(got) != ringSize {
		t.Fatalf("Recent after overflow returned %d entries, want %d", len(got), ringSize)
	}

	if want := "entry-5"; got[0].Text != want {
		t.Errorf("oldest surviving entry = %q, want %q", got[0].Text, want)
	}

	if want := "entry-304"; got[len(got)-1].Text != want {
		t.Errorf("newest entry = %q, want %q", got[len(got)-1].Text, want)
	}
}

func TestKindString(t *testing.T) {
	for kind, want := range map[Kind]string{KindKey: "KEY", KindAction: "ACT", KindEvent: "EVT", Kind(42): "???"} {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

// Event is called from the goEvery background tickers while gocui's goroutine
// is logging keys and the render goroutine is calling Recent — the reason the
// Logger carries a mutex at all.
func TestConcurrentLoggingAndReading(t *testing.T) {
	l, _ := newTestLogger(t)

	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				l.Event("writer-%d-%d", n, j)
			}
		}(i)
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		for j := 0; j < 200; j++ {
			_ = l.Recent(20)
		}
	}()

	wg.Wait()
}

func TestPathReportsTheOpenedFile(t *testing.T) {
	l, path := newTestLogger(t)

	if l.Path() != path {
		t.Errorf("Path() = %q, want %q", l.Path(), path)
	}
}
