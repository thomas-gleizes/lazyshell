package gui

import (
	"testing"
	"time"
)

func TestRefreshAgentStatsNoopWhenUnconfigured(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	newTestSession(t, gui, "s")

	if err := gui.refreshAgentStats(); err != nil {
		t.Fatalf("refreshAgentStats: %v", err)
	}

	if lines := gui.statsLinesForRender(); lines != nil {
		t.Errorf("statsLinesForRender() = %v, want nil with no AgentStatsCommand configured", lines)
	}
}

func TestRefreshAgentStatsCapturesFirstLineAndInjectsSessionID(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")

	gui.agentStatsCommand = `printf 'id=%s\nsecond line\n' "$LAZYSHELL_SESSION_ID"`

	if err := gui.refreshAgentStats(); err != nil {
		t.Fatalf("refreshAgentStats: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var lines map[string]string
	for time.Now().Before(deadline) {
		lines = gui.statsLinesForRender()
		if len(lines) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	want := "id=" + sess.ID
	if got := lines[sess.ID]; got != want {
		t.Errorf("statsLinesForRender()[%s] = %q, want %q (first line only, session id injected)", sess.ID, got, want)
	}
}

func TestRefreshAgentStatsThrottles(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	newTestSession(t, gui, "s")

	gui.agentStatsCommand = "echo hi"

	if err := gui.refreshAgentStats(); err != nil {
		t.Fatalf("refreshAgentStats: %v", err)
	}

	gui.mu.Lock()
	first := gui.statsCheckedAt
	gui.mu.Unlock()

	if first.IsZero() {
		t.Fatal("refreshAgentStats did not record statsCheckedAt")
	}

	if err := gui.refreshAgentStats(); err != nil {
		t.Fatalf("refreshAgentStats: %v", err)
	}

	gui.mu.Lock()
	second := gui.statsCheckedAt
	gui.mu.Unlock()

	if !second.Equal(first) {
		t.Fatal("a second refreshAgentStats call within statsRefreshInterval must be a no-op")
	}
}

func TestRefreshAgentStatsNoopWithNoSelection(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.agentStatsCommand = "echo hi"

	if err := gui.refreshAgentStats(); err != nil {
		t.Fatalf("refreshAgentStats with no sessions: %v", err)
	}

	if lines := gui.statsLinesForRender(); lines != nil {
		t.Errorf("statsLinesForRender() = %v, want nil", lines)
	}
}
