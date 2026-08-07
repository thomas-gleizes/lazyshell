package session

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/hook"
)

// $LAZYSHELL_SESSION_ID and $LAZYSHELL_SOCK are the two variables an agent's
// hook mechanism needs to correlate itself with a session and reach its
// socket — this is what makes `lazyshell hook <event>` work with zero
// configuration beyond whatever the agent's own hook config points at.
func TestNewSessionInjectsHookEnv(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "hook-env")

	if _, err := sess.Write([]byte("echo id=[$LAZYSHELL_SESSION_ID] sock=[$LAZYSHELL_SOCK]\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := waitForScreen(t, sess, "id=["+sess.ID+"]")

	if !strings.Contains(out, sess.ID+".sock") {
		t.Fatalf("screen = %q, want $LAZYSHELL_SOCK ending in %s.sock", out, sess.ID)
	}
}

// End-to-end through a real socket: sending a hook event the way `lazyshell
// hook <event>` does (hook.Send against $LAZYSHELL_SOCK) must reach this
// exact session's AgentState().
func TestHookEventReachesTheRightSession(t *testing.T) {
	m := newTestManager(t)
	a := newTestSession(t, m, "a")
	b := newTestSession(t, m, "b")

	waitForScreen(t, a, "$")
	waitForScreen(t, b, "$")

	sockA := readSockEnv(t, a)

	if err := hook.Send(sockA, agent.StateBlocked); err != nil {
		t.Fatalf("hook.Send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && a.AgentState() != agent.StateBlocked {
		time.Sleep(20 * time.Millisecond)
	}

	if got := a.AgentState(); got != agent.StateBlocked {
		t.Fatalf("a.AgentState() = %v, want StateBlocked", got)
	}

	if got := b.AgentState(); got != agent.StateNone {
		t.Fatalf("b.AgentState() = %v, want StateNone — the event must not leak across sessions", got)
	}
}

// The core 11b guarantee: once a hook has spoken for a session, the 11a
// manifest fallback must never override it again, even when the screen
// would otherwise match a rule with a different state.
func TestHookDrivenStateSurvivesConflictingManifestMatch(t *testing.T) {
	m := newTestManager(t)

	// A throwaway session, only to learn what foregroundProcessName actually
	// reports for testShell on this platform — m.Detector must be set
	// *before* the real test session is created, not mutated on it
	// afterwards: sess.detector is read from the drain goroutine with no
	// lock (it never changes after construction in production), so writing
	// it post-creation from the test goroutine would be a genuine data race.
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
				{State: agent.StateBlocked, ScreenPattern: regexp.MustCompile("CONFLICT-MARKER")},
			},
		},
	})

	sess := newTestSession(t, m, "hook-wins")
	waitForScreen(t, sess, "$")

	sess.SetAgentState(agent.StateDone)

	if _, err := sess.Write([]byte("echo CONFLICT-MARKER\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForScreen(t, sess, "CONFLICT-MARKER")

	// Give evaluateAgentState every chance to run and (wrongly) overwrite the
	// hook-driven state; agentCheckInterval is 500ms, so this comfortably
	// covers a throttled re-check too.
	time.Sleep(700 * time.Millisecond)

	if got := sess.AgentState(); got != agent.StateDone {
		t.Fatalf("AgentState() = %v after a conflicting manifest match, want the hook-driven StateDone to survive", got)
	}
}

// readSockEnv extracts $LAZYSHELL_SOCK from a session's own environment by
// asking its shell to print it. Session.Env() would be easier to read, but it
// reports what the process was *started* with; going through the shell proves
// the value actually reached it, which is what the hook channel depends on.
func readSockEnv(t *testing.T, sess *Session) string {
	t.Helper()

	if _, err := sess.Write([]byte("echo SOCK-START=[$LAZYSHELL_SOCK]=SOCK-END\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := waitForScreen(t, sess, "]=SOCK-END")

	// The pty echoes the typed command line too, which contains this same
	// marker text verbatim (unexpanded) — the real, expanded output is
	// whichever occurrence comes last on screen.
	marker := "SOCK-START=["
	start := strings.LastIndex(out, marker) + len(marker)
	end := strings.Index(out[start:], "]=SOCK-END")
	if end < 0 {
		t.Fatalf("could not find the closing marker in %q", out)
	}

	// The socket path is often wider than the 80-column test screen, so the
	// terminal soft-wraps it — a Unix path never contains whitespace, so
	// stripping every space/newline the wrap introduced is safe.
	return strings.Join(strings.Fields(out[start:start+end]), "")
}
