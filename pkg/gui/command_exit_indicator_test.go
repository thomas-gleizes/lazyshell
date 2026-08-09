package gui

import (
	"strings"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

// The sessions list's exit-code indicator: a non-agent session whose last
// command (per OSC 133 shell integration) exited non-zero gets "✗ <code>"
// prepended onto its detail column.
func TestCommandExitIndicatorShowsForNonAgentFailedCommand(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "plain-session")
	waitForScreen(t, sess, "$")

	if _, err := sess.Screen().Write([]byte("\x1b]133;A\x07$ \x1b]133;B\x07false\r\n\x1b]133;D;1\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	set := gui.markerSet()
	line := sessionLine(sess, set, "", nil, nil)
	if !strings.Contains(line, colorizeMarker(commandFailedMarker, "31")+" 1") {
		t.Errorf("sessionLine = %q, want the failed-command indicator", line)
	}
}

// A successful command must not light up the indicator — it exists to flag
// failure, not "a command ran".
func TestCommandExitIndicatorHiddenOnSuccess(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "plain-session")
	waitForScreen(t, sess, "$")

	if _, err := sess.Screen().Write([]byte("\x1b]133;A\x07$ \x1b]133;B\x07true\r\n\x1b]133;D;0\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	set := gui.markerSet()
	line := sessionLine(sess, set, "", nil, nil)
	if strings.Contains(line, commandFailedMarker) {
		t.Errorf("sessionLine = %q, indicator shown for a successful command", line)
	}
}

// A detected AI agent session already carries its own state marker in the
// gutter; the exit-code indicator must not pile a second, differently-sourced
// signal on top of it.
func TestCommandExitIndicatorHiddenForAgentSession(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "agent-session")
	waitForScreen(t, sess, "$")

	sess.SetAgentState(agent.StateWorking)

	if _, err := sess.Screen().Write([]byte("\x1b]133;A\x07$ \x1b]133;B\x07false\r\n\x1b]133;D;1\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	set := gui.markerSet()
	line := sessionLine(sess, set, "", nil, nil)
	if strings.Contains(line, commandFailedMarker) {
		t.Errorf("sessionLine = %q, indicator shown for a detected agent session", line)
	}
}

// The glyph is configurable like every other marker, and turning it off (the
// empty-string convention every marker shares) must suppress it even for an
// otherwise-qualifying failed command.
func TestCommandExitIndicatorOffTurnsItOff(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "plain-session")
	waitForScreen(t, sess, "$")

	if _, err := sess.Screen().Write([]byte("\x1b]133;A\x07$ \x1b]133;B\x07false\r\n\x1b]133;D;1\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	set := gui.markerSet()
	set.commandFailed = ""

	line := sessionLine(sess, set, "", nil, nil)
	if strings.Contains(line, "✗") {
		t.Errorf("sessionLine = %q, indicator shown despite an empty configured marker", line)
	}
}
