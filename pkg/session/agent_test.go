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

// The throttle is the whole point of running detection in the drain
// goroutine rather than the render loop: two evaluations closer together
// than agentCheckInterval must not both do the work of a foreground-process
// lookup and a manifest evaluation. White-box on purpose (same package): the
// only externally visible effect of skipping would be a state that lags by
// up to agentCheckInterval, which is not something a black-box test could
// distinguish from "detection just hasn't run yet".
func TestEvaluateAgentStateThrottles(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "throttle")
	sess.detector = agent.NewDetector(map[string]agent.Manifest{
		"sh": {Process: "sh"},
	})

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
	sess := newTestSession(t, m, "trailing-edge")
	sess.detector = agent.NewDetector(map[string]agent.Manifest{
		"sh": {Process: "sh"},
	})

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
