package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/control"
)

// The verbs split in two (see control.go's package-level comment): List, Read
// and Send run inline on the caller's goroutine, New/Kill/Rename go through
// onGUI. These tests exercise the inline half without a MainLoop, which is
// exactly what that split is supposed to make possible — if one of them ever
// starts needing gocui's goroutine, it will hang here rather than race in
// production.

func TestListReportsEverySession(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	a := newTestSession(t, gui, "alpha")
	newTestSession(t, gui, "beta")

	infos := gui.List()
	if len(infos) != 2 {
		t.Fatalf("List() returned %d sessions, want 2", len(infos))
	}

	if infos[0].ID != a.ID || infos[0].Name != "alpha" {
		t.Errorf("first session = %+v, want id %s named alpha", infos[0], a.ID)
	}

	if infos[0].Status != "running" {
		t.Errorf("status = %q, want running", infos[0].Status)
	}

	// StateNone has no spelling on purpose (agent.ParseState); a session
	// running no agent must report an empty state, never "none".
	if infos[0].AgentState != "" {
		t.Errorf("agent state = %q, want empty for a plain shell", infos[0].AgentState)
	}
}

func TestReadReturnsTheSessionOutputAsPlainText(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "t")
	feed(t, sess, "\x1b[38;5;82mmarqueur-controle\x1b[0m\r\n")

	out, err := gui.Read(sess.ID, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if !strings.Contains(out, "marqueur-controle") {
		t.Errorf("Read() = %q, want it to contain the fed text", out)
	}

	if strings.Contains(out, "38;5;82") {
		t.Error("Read() leaked an SGR sequence — it must return plain text")
	}
}

func TestReadTailLimitsTheOutput(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "t")
	feed(t, sess, "un\r\ndeux\r\ntrois\r\n")

	out, err := gui.Read(sess.ID, 1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if strings.Contains(out, "un") {
		t.Errorf("Read(tail 1) = %q, want only the last line", out)
	}
}

func TestSendWritesIntoTheSession(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	// cat echoes back whatever is written to the pty, which is what makes the
	// write observable without waiting on a shell prompt.
	sess := newSilentTestSession(t, gui, "t")

	if err := gui.Send(sess.ID, "salut-controle\r"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sess.Screen().PlainTail(5), "salut-controle") {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("sent text never reached the session, screen = %q", sess.Screen().PlainTail(5))
}

func TestResolveSessionByIDByNameAndUnknown(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "alpha")

	if got, err := gui.resolveSession(sess.ID); err != nil || got != sess {
		t.Errorf("resolveSession(id) = %v, %v", got, err)
	}

	if got, err := gui.resolveSession("alpha"); err != nil || got != sess {
		t.Errorf("resolveSession(name) = %v, %v", got, err)
	}

	if _, err := gui.resolveSession("fantome"); err == nil {
		t.Error("resolveSession on an unknown session returned no error")
	}

	if _, err := gui.resolveSession(""); err == nil {
		t.Error("resolveSession(\"\") returned no error")
	}
}

// Nothing pumps g.Update in a headless Gui unless the test runs a MainLoop, so
// this is the wedged-event-loop case: the caller must get an error, not block
// until pkg/control's own timeout fires.
func TestOnGUIFailsRatherThanHangingWithNoEventLoop(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	done := make(chan error, 1)
	go func() { done <- gui.onGUI(func() error { return nil }) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("onGUI succeeded with no event loop running")
		}
	case <-time.After(onGUITimeout + 2*time.Second):
		t.Fatal("onGUI hung instead of timing out")
	}
}

func TestOnGUIWithoutAGuiInstance(t *testing.T) {
	gui := New(nil, config.Default())

	if err := gui.onGUI(func() error { return nil }); err == nil {
		t.Error("onGUI on a Gui with no gocui instance returned no error")
	}
}

// tempSocketPath is pkg/control's helper, repeated here for the same reason:
// t.TempDir() embeds the test's name in the path and a Unix socket path is
// capped at ~104 bytes on macOS, which these test names blow straight past.
func tempSocketPath(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ctl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return filepath.Join(dir, "c.sock")
}

// The toggle is the whole security story of this feature: off must mean no
// socket at all, not merely a socket nobody was told about.
func TestControlServerDoesNotStartWhenDisabled(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	srv, err := gui.startControlServer(tempSocketPath(t))
	if err != nil {
		t.Fatalf("startControlServer: %v", err)
	}

	if srv != nil {
		_ = srv.Close()
		t.Fatal("control socket opened even though control.enabled is false")
	}
}

func TestControlServerStartsWhenEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Control.Enabled = true
	gui, _ := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	path := tempSocketPath(t)

	srv, err := gui.startControlServer(path)
	if err != nil {
		t.Fatalf("startControlServer: %v", err)
	}

	if srv == nil {
		t.Fatal("control.enabled is true but no socket was opened")
	}
	defer func() { _ = srv.Close() }()

	// End to end through the real socket: the adapter is reachable as a
	// control.Handler, not merely assignable to one.
	newTestSession(t, gui, "alpha")

	resp, err := control.Call(path, control.Request{Verb: control.VerbList})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if !resp.OK || len(resp.Sessions) != 1 || resp.Sessions[0].Name != "alpha" {
		t.Errorf("list over the socket = %+v", resp)
	}
}

// Compile-time proof that the split above is a complete implementation of the
// interface pkg/control dispatches against.
var _ control.Handler = (*Gui)(nil)
