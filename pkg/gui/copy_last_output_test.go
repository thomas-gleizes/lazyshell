package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyLastCommandOutput bypasses copy-mode entirely: OSC 133's C..D range
// (or B..D when a command never produced a C) goes straight to the
// clipboard dispatch copymode.go's yankCopySelection already uses.
func TestCopyLastCommandOutputCopiesTheRange(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	sess := gui.selectedSession()
	waitForSessionScreen(t, gui, "$")

	scr := sess.Screen()
	if _, err := scr.Write([]byte("\x1b]133;A\x07$ echo hi\r\n\x1b]133;C\x07hi\r\n\x1b]133;D;0\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "clip.txt")
	gui.clipboardFallback = "cat > " + tmpFile

	gui.copyLastCommandOutput()

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("reading fallback output: %v", err)
	}

	if !strings.Contains(string(data), "hi") {
		t.Errorf("copied text = %q, want it to contain the command's output %q", data, "hi")
	}
}

// With no command having finished yet, this reports a status message rather
// than silently doing nothing or invoking the clipboard with garbage.
func TestCopyLastCommandOutputNoCommandYetReportsStatus(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	waitForSessionScreen(t, gui, "$")

	tmpFile := filepath.Join(t.TempDir(), "clip.txt")
	gui.clipboardFallback = "cat > " + tmpFile

	gui.copyLastCommandOutput()

	if gui.lastError == "" {
		t.Error("lastError is empty, want a status message when no command has finished")
	}

	if _, err := os.Stat(tmpFile); err == nil {
		t.Error("the clipboard fallback command ran despite there being nothing to copy")
	}
}
