package gui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

func TestWorkingAgentSessionNamesFiltersToStateWorkingOnly(t *testing.T) {
	m := session.NewManager()
	m.KillTimeout = 1 * time.Second
	t.Cleanup(m.Shutdown)

	none, err := m.New("none", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	idle, err := m.New("idle", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	working, err := m.New("working", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocked, err := m.New("blocked", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done, err := m.New("done", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = none

	idle.SetAgentState(agent.StateIdle)
	working.SetAgentState(agent.StateWorking)
	blocked.SetAgentState(agent.StateBlocked)
	done.SetAgentState(agent.StateDone)

	gui := &Gui{sessions: m}

	got := gui.workingAgentSessionNames()
	if len(got) != 1 || got[0] != "working" {
		t.Errorf("workingAgentSessionNames() = %v, want [\"working\"] — idle/blocked/done/none must not count as work in flight", got)
	}
}

func TestQuitConfirmMessageEmptyWithoutWorkingAgent(t *testing.T) {
	gui := New(session.NewManager(), config.Default())

	if msg, ok := gui.quitConfirmMessage(); ok || msg != "" {
		t.Errorf("quitConfirmMessage() = (%q, %v), want (\"\", false) with no agent working", msg, ok)
	}
}

func TestQuitConfirmMessageNamesTheWorkingSessions(t *testing.T) {
	m := session.NewManager()
	m.KillTimeout = 1 * time.Second
	t.Cleanup(m.Shutdown)

	sess, err := m.New("my-agent", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess.SetAgentState(agent.StateWorking)

	gui := New(m, config.Default())

	msg, ok := gui.quitConfirmMessage()
	if !ok {
		t.Fatal("quitConfirmMessage() ok = false, want true with an agent working")
	}
	if !strings.Contains(msg, "my-agent") {
		t.Errorf("quitConfirmMessage() = %q, want it to name the working session", msg)
	}
}

// TestQuitReturnsErrQuitWithoutWorkingAgent guards the common case: no agent
// running, "q" must quit immediately with no popup in the way.
func TestQuitReturnsErrQuitWithoutWorkingAgent(t *testing.T) {
	gui := New(session.NewManager(), config.Default())

	if err := gui.quit(nil, nil); err != gocui.ErrQuit {
		t.Errorf("quit() = %v, want gocui.ErrQuit", err)
	}
}

// TestQuitOpensConfirmWhenAgentIsWorking is this feature's core behaviour:
// an in-flight agent turn must not be silently killed by "q".
func TestQuitOpensConfirmWhenAgentIsWorking(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess, err := gui.sessions.New("my-agent", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess.SetAgentState(agent.StateWorking)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.quit(g, nil); err != nil {
		t.Fatalf("quit() = %v, want nil (a popup, not an immediate quit)", err)
	}

	view, err := g.View(confirmViewName)
	if err != nil {
		t.Fatalf("confirm popup not shown: %v", err)
	}
	if !strings.Contains(view.Buffer(), "my-agent") {
		t.Errorf("confirm popup = %q, want it to name the working session", view.Buffer())
	}
}

// TestQuitConfirmAcceptedActuallyQuits is confirm.go's critical fix: an
// onConfirm returning gocui.ErrQuit must reach the caller unchanged instead
// of being swallowed into lastError, or accepting the popup would silently
// do nothing.
func TestQuitConfirmAcceptedActuallyQuits(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if _, err := gui.sessions.New("s0", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.showConfirm("quit? (y/n)", func() error { return gocui.ErrQuit }); err != nil {
		t.Fatalf("showConfirm: %v", err)
	}

	if err := gui.acceptConfirm(func() error { return gocui.ErrQuit }); err != gocui.ErrQuit {
		t.Errorf("acceptConfirm() = %v, want gocui.ErrQuit to propagate to MainLoop", err)
	}
	if gui.lastError != "" {
		t.Errorf("lastError = %q, want empty — ErrQuit is not a failure to report", gui.lastError)
	}
}

// TestQuitConfirmAcceptedReportsARealFailure guards the branch beside the
// ErrQuit special case: an onConfirm failing for an ordinary reason must
// still show up in the status bar, same as every other confirm dialog.
func TestQuitConfirmAcceptedReportsARealFailure(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if _, err := gui.sessions.New("s0", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.showConfirm("proceed? (y/n)", func() error { return nil }); err != nil {
		t.Fatalf("showConfirm: %v", err)
	}

	boom := errors.New("boom")
	if err := gui.acceptConfirm(func() error { return boom }); err != nil {
		t.Errorf("acceptConfirm() = %v, want nil — an ordinary failure must not propagate as MainLoop's exit error", err)
	}
	if gui.lastError != boom.Error() {
		t.Errorf("lastError = %q, want %q", gui.lastError, boom.Error())
	}
}

// TestRequestQuitFromEditorOpensConfirmWhenAgentIsWorking covers the second
// quit path — the Editable views' hand-matched "q" (see input.go) — which
// cannot return gocui.ErrQuit directly and so must go through the same
// agent check by hand.
func TestRequestQuitFromEditorOpensConfirmWhenAgentIsWorking(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess, err := gui.sessions.New("my-agent", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess.SetAgentState(agent.StateWorking)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.requestQuitFromEditor()

	view, err := g.View(confirmViewName)
	if err != nil {
		t.Fatalf("confirm popup not shown: %v", err)
	}
	if !strings.Contains(view.Buffer(), "my-agent") {
		t.Errorf("confirm popup = %q, want it to name the working session", view.Buffer())
	}
}
