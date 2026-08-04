package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/keys"
)

// ptyReader accumulates everything the shell writes, so a test can wait for a
// marker without racing the reader goroutine.
type ptyReader struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *ptyReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.buf.Write(p)
}

func (r *ptyReader) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.buf.String()
}

// waitFor polls the accumulated output until it contains want.
func (r *ptyReader) waitFor(t *testing.T, want string) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if out := r.String(); strings.Contains(out, want) {
			return out
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q in shell output:\n%s", want, r.String())

	return ""
}

// startShell runs /bin/sh behind a pty of the given size and streams its output
// into the returned reader.
func startShell(t *testing.T, rows, cols uint16) (*os.File, *ptyReader) {
	t.Helper()

	cmd := exec.Command("/bin/sh")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "PS1=")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		t.Fatalf("pty.StartWithSize: %v", err)
	}

	reader := &ptyReader{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(reader, ptmx)
	}()

	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		<-done
	})

	return ptmx, reader
}

// typeKeys writes a string the way the editor does: one gocui event per rune,
// translated to bytes.
func typeKeys(t *testing.T, ptmx *os.File, text string) {
	t.Helper()

	for _, ch := range text {
		if _, err := ptmx.Write(keys.Translate(0, ch, gocui.ModNone)); err != nil {
			t.Fatalf("write %q: %v", ch, err)
		}
	}
}

// pressKey writes a single non-printable key.
func pressKey(t *testing.T, ptmx *os.File, key gocui.Key) {
	t.Helper()

	if _, err := ptmx.Write(keys.Translate(key, 0, gocui.ModNone)); err != nil {
		t.Fatalf("write key %d: %v", key, err)
	}
}

// The core phase 1 hypothesis: keys translated by pkg/keys, written to a pty,
// are understood by a real shell.
func TestTranslatedKeysDriveAShell(t *testing.T) {
	ptmx, out := startShell(t, 24, 80)

	typeKeys(t, ptmx, "echo lazyshell-ok")
	pressKey(t, ptmx, gocui.KeyEnter)

	out.waitFor(t, "lazyshell-ok")
}

// Roadmap exit criterion: `stty size` inside the session must report the size
// we gave the pty, not the size of the host terminal.
func TestPtySizeIsPropagated(t *testing.T) {
	const (
		rows = 31
		cols = 97
	)

	ptmx, out := startShell(t, rows, cols)

	typeKeys(t, ptmx, "stty size")
	pressKey(t, ptmx, gocui.KeyEnter)

	out.waitFor(t, "31 97")
}

// A resize after startup must reach the running shell too.
func TestPtyResizeReachesTheShell(t *testing.T) {
	ptmx, out := startShell(t, 24, 80)

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}

	typeKeys(t, ptmx, "stty size")
	pressKey(t, ptmx, gocui.KeyEnter)

	out.waitFor(t, "40 120")
}

// Ctrl-C must interrupt the foreground job rather than reach lazyshell.
func TestCtrlCInterruptsTheForegroundJob(t *testing.T) {
	ptmx, out := startShell(t, 24, 80)

	typeKeys(t, ptmx, "sleep 30; echo interrupted")
	pressKey(t, ptmx, gocui.KeyEnter)

	// Give the shell time to actually start the sleep before interrupting.
	time.Sleep(300 * time.Millisecond)
	pressKey(t, ptmx, gocui.KeyCtrlC)

	out.waitFor(t, "interrupted")
}
