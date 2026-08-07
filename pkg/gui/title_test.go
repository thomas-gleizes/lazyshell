package gui

import (
	"strings"
	"testing"
)

func TestOscTitleWriteEmitsOSC0(t *testing.T) {
	read := redirectStdout(t)

	oscTitleWrite("hello")

	got := read()
	if got != "\x1b]0;hello\x07" {
		t.Errorf("output = %q, want OSC 0 with %q", got, "hello")
	}
}

func TestUpdateWindowTitleUsesSessionNameAndLiveOSCTitle(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "mysession")

	read := redirectStdout(t)
	gui.updateWindowTitle()

	got := read()
	if !strings.Contains(got, "\x1b]0;lazyshell — mysession\x07") {
		t.Fatalf("output = %q, want session name only (no live title set)", got)
	}

	feed(t, sess, "\x1b]0;vim ROADMAP.md\x07")

	read = redirectStdout(t)
	gui.updateWindowTitle()

	got = read()
	if !strings.Contains(got, "\x1b]0;lazyshell — mysession: vim ROADMAP.md\x07") {
		t.Fatalf("output = %q, want session name combined with the live OSC title", got)
	}
}

func TestUpdateWindowTitleDoesNotRewriteUnchangedTitle(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	newTestSession(t, gui, "mysession")

	gui.updateWindowTitle()

	read := redirectStdout(t)
	gui.updateWindowTitle()

	if got := read(); got != "" {
		t.Fatalf("updateWindowTitle rewrote an unchanged title: %q", got)
	}
}

func TestUpdateWindowTitleNoopWhenDisabled(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.windowTitleEnabled = false
	newTestSession(t, gui, "mysession")

	read := redirectStdout(t)
	gui.updateWindowTitle()
	if got := read(); got != "" {
		t.Fatalf("updateWindowTitle wrote while disabled: %q", got)
	}

	gui.windowTitleEnabled = true
	read = redirectStdout(t)
	gui.updateWindowTitle()
	if got := read(); got == "" {
		t.Fatalf("updateWindowTitle wrote nothing once re-enabled with a session selected")
	}
}

func TestUpdateWindowTitleNoopWithNoSelection(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	read := redirectStdout(t)
	gui.updateWindowTitle()
	if got := read(); got != "" {
		t.Fatalf("updateWindowTitle wrote with no session selected: %q", got)
	}
}

func TestResetWindowTitleClearsUnlessDisabled(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	read := redirectStdout(t)
	gui.resetWindowTitle()
	if got := read(); got != "\x1b]0;\x07" {
		t.Fatalf("resetWindowTitle output = %q, want an empty OSC 0 title", got)
	}

	gui.windowTitleEnabled = false
	read = redirectStdout(t)
	gui.resetWindowTitle()
	if got := read(); got != "" {
		t.Fatalf("resetWindowTitle wrote while disabled: %q", got)
	}
}
