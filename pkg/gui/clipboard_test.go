package gui

import (
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// redirectStdout temporarily replaces os.Stdout with a pipe, restored via
// t.Cleanup, and returns a function that reads whatever was written to it so
// far. oscClipboardWrite has nowhere else to write a real OSC 52 sequence to
// — it must be the process's actual stdout, not a gocui.View — so this is the
// only way to observe it from a test.
func redirectStdout(t *testing.T) func() string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	original := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = original })

	return func() string {
		w.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("reading redirected stdout: %v", err)
		}

		return string(out)
	}
}

func TestOscClipboardWriteEmitsOSC52(t *testing.T) {
	read := redirectStdout(t)

	if err := oscClipboardWrite("hello"); err != nil {
		t.Fatalf("oscClipboardWrite: %v", err)
	}

	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello")) + "\x07"
	if got := read(); got != want {
		t.Errorf("oscClipboardWrite output = %q, want %q", got, want)
	}
}

func TestCopyToClipboardUsesOSC52WhenNoFallbackConfigured(t *testing.T) {
	read := redirectStdout(t)

	gui := &Gui{}

	if err := gui.copyToClipboard("no fallback"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}

	if got := read(); got == "" {
		t.Error("copyToClipboard with no fallback wrote nothing to stdout")
	}
}

func TestCopyToClipboardPrefersFallbackCommandWhenConfigured(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "clip.txt")

	gui := &Gui{clipboardFallback: "cat > " + tmpFile}

	if err := gui.copyToClipboard("via fallback"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("reading fallback output: %v", err)
	}

	if string(data) != "via fallback" {
		t.Errorf("fallback command received %q, want %q", data, "via fallback")
	}
}

func TestCopyToClipboardReportsFallbackCommandFailure(t *testing.T) {
	gui := &Gui{clipboardFallback: "exit 1"}

	if err := gui.copyToClipboard("text"); err == nil {
		t.Error("copyToClipboard did not report a failing fallback command")
	}
}
