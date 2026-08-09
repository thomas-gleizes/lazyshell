package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

func TestOscNotifyWriteEmitsOSC9AndOSC777(t *testing.T) {
	read := redirectStdout(t)

	oscNotifyWrite("hello")

	got := read()
	if !strings.Contains(got, "\x1b]9;hello\x07") {
		t.Errorf("output = %q, missing OSC 9", got)
	}
	if !strings.Contains(got, "\x1b]777;notify;lazyshell;hello\x07") {
		t.Errorf("output = %q, missing OSC 777", got)
	}
}

func TestCheckAgentNotificationsFiresOnceAndOnlyOnTransitionIntoBlockedOrDone(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	read := redirectStdout(t)

	// Idle: nothing to notify.
	if err := gui.checkAgentNotifications(); err != nil {
		t.Fatalf("checkAgentNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification fired on an idle session: %q", got)
	}

	read = redirectStdout(t)
	sess.SetAgentState(agent.StateBlocked)

	if err := gui.checkAgentNotifications(); err != nil {
		t.Fatalf("checkAgentNotifications: %v", err)
	}
	if got := read(); !strings.Contains(got, "\x1b]9;") {
		t.Fatalf("no notification fired on the idle -> blocked transition: %q", got)
	}

	// Same state again: must not re-fire.
	read = redirectStdout(t)
	if err := gui.checkAgentNotifications(); err != nil {
		t.Fatalf("checkAgentNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification re-fired for an unchanged state: %q", got)
	}
}

func TestCheckAgentNotificationsUsesFallbackCommandWhenConfigured(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	tmpFile := filepath.Join(t.TempDir(), "notif.txt")
	gui.notifyFallback = "cat > " + tmpFile

	sess.SetAgentState(agent.StateDone)

	if err := gui.checkAgentNotifications(); err != nil {
		t.Fatalf("checkAgentNotifications: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(tmpFile); err == nil && strings.Contains(string(data), sess.Name()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("fallback command never wrote the notification file")
}

// The fallback command runs in its own goroutine, so a slow one must not
// make checkAgentNotifications itself slow — unlike copy-mode's synchronous
// yank, this fires from a shared periodic tick.
func TestCheckAgentNotificationsFallbackDoesNotBlock(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	gui.notifyFallback = "sleep 2"
	sess.SetAgentState(agent.StateBlocked)

	start := time.Now()
	if err := gui.checkAgentNotifications(); err != nil {
		t.Fatalf("checkAgentNotifications: %v", err)
	}

	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("checkAgentNotifications took %v, want it to return well before the fallback command finishes", elapsed)
	}
}

func writeOsc133Command(t *testing.T, sess *session.Session, exitCode string) {
	t.Helper()

	seq := "\x1b]133;A\x07$ \x1b]133;B\x07cmd\r\n\x1b]133;D"
	if exitCode != "" {
		seq += ";" + exitCode
	}
	seq += "\x07"

	if _, err := sess.Screen().Write([]byte(seq)); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// A non-agent session's failed command fires exactly one notification, never
// a repeat for the same command.
func TestCheckCommandExitNotificationsFiresOnceForNonAgentFailure(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	writeOsc133Command(t, sess, "1")

	read := redirectStdout(t)
	if err := gui.checkCommandExitNotifications(); err != nil {
		t.Fatalf("checkCommandExitNotifications: %v", err)
	}
	if got := read(); !strings.Contains(got, "\x1b]9;") {
		t.Fatalf("no notification fired for a non-agent session's failed command: %q", got)
	}

	// Same command again (seq unchanged): must not re-fire.
	read = redirectStdout(t)
	if err := gui.checkCommandExitNotifications(); err != nil {
		t.Fatalf("checkCommandExitNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification re-fired for the same finished command: %q", got)
	}
}

// A successful command must never notify — this feature is about failure,
// not "a command ran".
func TestCheckCommandExitNotificationsSkipsSuccess(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	writeOsc133Command(t, sess, "0")

	read := redirectStdout(t)
	if err := gui.checkCommandExitNotifications(); err != nil {
		t.Fatalf("checkCommandExitNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification fired for a successful command: %q", got)
	}
}

// A detected AI agent session already gets its own blocked/done
// notifications; a failed command inside one must not double up.
func TestCheckCommandExitNotificationsSkipsAgentSessions(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")
	sess.SetAgentState(agent.StateWorking)

	writeOsc133Command(t, sess, "1")

	read := redirectStdout(t)
	if err := gui.checkCommandExitNotifications(); err != nil {
		t.Fatalf("checkCommandExitNotifications: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("notification fired for a detected agent session: %q", got)
	}
}
