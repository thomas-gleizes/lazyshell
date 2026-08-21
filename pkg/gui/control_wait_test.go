package gui

import (
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/control"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// Wait is inline (see control.go's package-level comment), so — like
// List/Read/Send — these run with no MainLoop: if Wait ever started needing
// gocui's goroutine, it would hang here rather than race in production.

func TestWaitReturnsImmediatelyWhenAlreadyInState(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "alpha")
	sess.SetAgentState(agent.StateBlocked)

	done := make(chan error, 1)

	go func() {
		_, err := gui.Wait(sess.ID, "", "blocked", 2*time.Second)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
	case <-time.After(waitPollInterval):
		t.Fatal("Wait did not return instantly when the state was already satisfied")
	}
}

func TestWaitUnblocksWhenStateChanges(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "alpha")

	type result struct {
		info control.SessionInfo
		err  error
	}

	done := make(chan result, 1)

	go func() {
		info, err := gui.Wait(sess.ID, "", "blocked", 2*time.Second)
		done <- result{info, err}
	}()

	time.Sleep(50 * time.Millisecond)
	sess.SetAgentState(agent.StateBlocked)

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Wait: %v", r.err)
		}

		if r.info.ID != sess.ID || r.info.AgentState != "blocked" {
			t.Errorf("Wait returned %+v, want session %s in state blocked", r.info, sess.ID)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not unblock after the state changed")
	}
}

// The regression this pins: a wait that legitimately runs its full length
// must be answered by the server with a structured Response{OK:false}, never
// by the client's own transport deadline firing first — see
// control.callDeadline's doc comment.
func TestWaitTimesOutWithoutATransportError(t *testing.T) {
	cfg := config.Default()
	cfg.Control.Enabled = true
	gui, _ := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	newTestSession(t, gui, "alpha")

	path := tempSocketPath(t)

	srv, err := gui.startControlServer(path)
	if err != nil {
		t.Fatalf("startControlServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	start := time.Now()

	resp, err := control.Call(path, control.Request{
		Verb: control.VerbWait, ID: "alpha", State: "blocked", Timeout: 1,
	})
	if err != nil {
		t.Fatalf("Call returned a transport error, want a structured timeout response: %v", err)
	}

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("wait took %s to answer a 1 s timeout", elapsed)
	}

	if resp.OK {
		t.Fatal("OK = true, want false: the session never reached the requested state")
	}
}

func TestWaitFailsWhenTheTargetedSessionExitsFirst(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess, err := gui.sessions.NewWithOptions(session.Options{Name: "short", Shell: "/usr/bin/true"})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session never exited")
	}

	if _, err := gui.Wait(sess.ID, "", "done", 2*time.Second); err == nil {
		t.Error("Wait on a session that exited before reaching the state did not error")
	}
}

func TestWaitGroupReturnsTheFirstMemberToMatch(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	a := newTestSession(t, gui, "a")
	a.SetGroup("agents")
	b := newTestSession(t, gui, "b")
	b.SetGroup("agents")

	b.SetAgentState(agent.StateBlocked)

	info, err := gui.Wait("", "agents", "blocked", 2*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if info.ID != b.ID {
		t.Errorf("Wait reported %s, want the member that actually matched (%s)", info.ID, b.ID)
	}
}

func TestWaitRejectsUnknownSessionGroupOrState(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	newTestSession(t, gui, "alpha")

	if _, err := gui.Wait("fantome", "", "blocked", time.Second); err == nil {
		t.Error("Wait on an unknown session did not error")
	}

	if _, err := gui.Wait("", "fantome", "blocked", time.Second); err == nil {
		t.Error("Wait on an unknown group did not error")
	}

	if _, err := gui.Wait("alpha", "", "surexcite", time.Second); err == nil {
		t.Error("Wait on an unparseable state did not error")
	}
}
