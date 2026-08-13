package session

import (
	"regexp"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

// A Manager with no Detector wired (NewManager's zero value) must report
// StateNone for every session, exactly like a session running a process no
// manifest recognizes — this is what makes agent detection safe to bolt onto
// every existing Manager without touching any of them.
func TestSessionAgentStateWithoutDetectorIsNone(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "no-detector")

	if got := sess.AgentState(); got != agent.StateNone {
		t.Fatalf("AgentState() = %v, want StateNone with no Detector wired", got)
	}

	waitForScreen(t, sess, "$")

	if got := sess.AgentState(); got != agent.StateNone {
		t.Fatalf("AgentState() = %v after output, want StateNone with no Detector wired", got)
	}
}

// End-to-end through a real pty: the session's foreground process is the
// shell itself, so a manifest for whatever name that process actually
// reports (testShell is "/bin/sh", but that resolves to "bash" or "dash"
// depending on the platform — foregroundProcessName is the source of truth,
// not the argv lazyshell started it with) whose working rule matches
// something the shell actually prints must flip AgentState() to Working
// without any extra wiring.
func TestSessionAgentStateDetectsViaManifest(t *testing.T) {
	m := newTestManager(t)
	probe := newTestSession(t, m, "probe")
	waitForScreen(t, probe, "$")

	processName, err := foregroundProcessName(probe.ptmx)
	if err != nil {
		t.Fatalf("foregroundProcessName: %v", err)
	}

	m.Detector = agent.NewDetector(map[string]agent.Manifest{
		processName: {
			Process: processName,
			Rules: []agent.Rule{
				{State: agent.StateWorking, ScreenPattern: regexp.MustCompile("READY-MARKER")},
			},
		},
	})

	sess := newTestSession(t, m, "agent-session")
	waitForScreen(t, sess, "$")

	if _, err := sess.Write([]byte("echo READY-MARKER\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitForScreen(t, sess, "READY-MARKER")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sess.AgentState() == agent.StateWorking {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("AgentState() never reached StateWorking, got %v", sess.AgentState())
}

// AgentName is derived purely from which manifest's process name matched,
// independent of any rule evaluation — so it must come back populated
// alongside AgentState once a manifest is wired.
func TestSessionAgentNameDetectsViaManifest(t *testing.T) {
	m := newTestManager(t)
	probe := newTestSession(t, m, "probe")
	waitForScreen(t, probe, "$")

	processName, err := foregroundProcessName(probe.ptmx)
	if err != nil {
		t.Fatalf("foregroundProcessName: %v", err)
	}

	m.Detector = agent.NewDetector(map[string]agent.Manifest{
		processName: {Process: processName},
	})

	sess := newTestSession(t, m, "agent-session")
	waitForScreen(t, sess, "$")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sess.AgentName() == processName {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("AgentName() = %q, want %q", sess.AgentName(), processName)
}

// SetAgentState makes a session hookDriven, which stops evaluateAgentState's
// rule-based state guessing — but not the separate, cheaper process-name
// lookup that feeds AgentName, since the hook channel itself never carries an
// agent identity (see pkg/hook's package doc). A hook-driven Claude Code
// session must still show up as "claude" in the dashboard.
func TestSessionAgentNameKeepsUpdatingOnceHookDriven(t *testing.T) {
	m := newTestManager(t)
	probe := newTestSession(t, m, "probe")
	waitForScreen(t, probe, "$")

	processName, err := foregroundProcessName(probe.ptmx)
	if err != nil {
		t.Fatalf("foregroundProcessName: %v", err)
	}

	m.Detector = agent.NewDetector(map[string]agent.Manifest{
		processName: {Process: processName},
	})

	sess := newTestSession(t, m, "hook-driven")
	waitForScreen(t, sess, "$")

	sess.SetAgentState(agent.StateWorking)

	if got := sess.AgentState(); got != agent.StateWorking {
		t.Fatalf("AgentState() = %v, want StateWorking (hook-driven)", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sess.AgentName() == processName {
			// The hook event must not have been clobbered by the
			// manifest-based guesswork in the meantime.
			if got := sess.AgentState(); got != agent.StateWorking {
				t.Fatalf("AgentState() = %v after AgentName caught up, want StateWorking to still hold", got)
			}

			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("AgentName() = %q, want %q even once hookDriven", sess.AgentName(), processName)
}

// The throttle is the whole point of running detection in the drain
// goroutine rather than the render loop: two evaluations closer together
// than agentCheckInterval must not both do the work of a foreground-process
// lookup and a manifest evaluation. White-box on purpose (same package): the
// only externally visible effect of skipping would be a state that lags by
// up to agentCheckInterval, which is not something a black-box test could
// distinguish from "detection just hasn't run yet".
func TestEvaluateAgentStateThrottles(t *testing.T) {
	m := newTestManager(t)
	m.Detector = agent.NewDetector(map[string]agent.Manifest{
		"sh": {Process: "sh"},
	})
	sess := newTestSession(t, m, "throttle")

	sess.evaluateAgentState()
	first := sess.lastAgentCheck

	if first.IsZero() {
		t.Fatal("evaluateAgentState did not record lastAgentCheck")
	}

	sess.evaluateAgentState()

	if !sess.lastAgentCheck.Equal(first) {
		t.Fatal("a second evaluateAgentState call within agentCheckInterval must be a no-op")
	}
}

// A check skipped by the throttle must not be lost forever: it is exactly
// the situation a session goes quiet in right after — a permission prompt
// printed, then nothing more until the user answers — so a deferred
// re-check must land once the throttle window closes, even with no further
// writes to trigger one.
func TestEvaluateAgentStateTrailingEdgeRecheck(t *testing.T) {
	m := newTestManager(t)
	m.Detector = agent.NewDetector(map[string]agent.Manifest{
		"sh": {Process: "sh"},
	})
	sess := newTestSession(t, m, "trailing-edge")

	sess.evaluateAgentState()
	first := sess.lastAgentCheck

	sess.evaluateAgentState() // throttled: arms the deferred re-check

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess.mu.Lock()
		last := sess.lastAgentCheck
		sess.mu.Unlock()

		if last.After(first) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("no deferred re-check ran after the throttle window closed")
}

func TestTurnDurationBeforeAnyTurnIsFalse(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "no-turn-yet")

	if _, ok := sess.TurnDuration(); ok {
		t.Fatal("TurnDuration() ok = true before any transition into StateWorking")
	}
}

func TestTurnDurationWhileWorkingIsTrue(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "mid-turn")

	sess.SetAgentState(agent.StateWorking)
	time.Sleep(10 * time.Millisecond)

	d, ok := sess.TurnDuration()
	if !ok {
		t.Fatal("TurnDuration() ok = false while StateWorking")
	}
	if d < 10*time.Millisecond {
		t.Errorf("TurnDuration() = %v, want at least 10ms", d)
	}
}

func TestTurnDurationDoesNotResetOnRepeatedWorking(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "repeated-working")

	sess.SetAgentState(agent.StateWorking)
	time.Sleep(30 * time.Millisecond)
	sess.SetAgentState(agent.StateWorking) // must not restart the clock

	d, ok := sess.TurnDuration()
	if !ok {
		t.Fatal("TurnDuration() ok = false while StateWorking")
	}
	if d < 30*time.Millisecond {
		t.Errorf("TurnDuration() = %v after a repeated working report, want the clock to have kept running (>= 30ms)", d)
	}
}

func TestTurnDurationFalseAfterLeavingWorking(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "turn-ended")

	sess.SetAgentState(agent.StateWorking)
	sess.SetAgentState(agent.StateDone)

	if _, ok := sess.TurnDuration(); ok {
		t.Fatal("TurnDuration() ok = true after leaving StateWorking, want false")
	}
}
