package gui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// waitForPrompt waits for /bin/sh's prompt, whichever shell it actually
// resolves to (bash, dash, sh, zsh) picks: "$ " for a normal user, "# " for
// root — this container runs the test suite as root, so waitForScreen's
// literal "$" never shows.
func waitForPrompt(t *testing.T, sess *session.Session) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		screen := sess.Screen().Render()
		if strings.HasSuffix(strings.TrimRight(screen, " \n"), "$") || strings.HasSuffix(strings.TrimRight(screen, " \n"), "#") {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("session %s never showed a shell prompt on screen:\n%s", sess.Name(), sess.Screen().Render())
}

func TestAgentMarkerMapsEveryStateToAGlyphAndColor(t *testing.T) {
	set := markerSet{agentIdle: "i", agentWorking: "w", agentBlocked: "b", agentDone: "d"}

	cases := []struct {
		state agent.State
		glyph string
	}{
		{agent.StateIdle, "i"},
		{agent.StateWorking, "w"},
		{agent.StateBlocked, "b"},
		{agent.StateDone, "d"},
	}

	seen := map[string]bool{}
	for _, c := range cases {
		glyph, code := set.agentMarker(c.state)
		if glyph != c.glyph {
			t.Errorf("agentMarker(%v) glyph = %q, want %q", c.state, glyph, c.glyph)
		}
		if code == "" {
			t.Errorf("agentMarker(%v) has no color code", c.state)
		}
		if seen[code] {
			t.Errorf("agentMarker(%v) reuses color code %q already seen for another state", c.state, code)
		}
		seen[code] = true
	}

	if glyph, code := set.agentMarker(agent.StateNone); glyph != "" || code != "" {
		t.Errorf("agentMarker(StateNone) = (%q, %q), want empty (no marker for a non-agent session)", glyph, code)
	}
}

func TestAgentMarkerUsesConfiguredColor(t *testing.T) {
	set := markerSet{agentIdle: "i", agentIdleColor: "#ff0000"}

	if _, code := set.agentMarker(agent.StateIdle); code != "38;2;255;0;0" {
		t.Errorf("agentMarker with agent_idle_color = %q, code = %q, want %q", "#ff0000", code, "38;2;255;0;0")
	}
}

func TestAgentMarkerPulsesOnlyWorkingState(t *testing.T) {
	bright := markerSet{agentIdle: "i", agentWorking: "w", agentBlocked: "b", agentDone: "d", pulseOn: true}
	dim := bright
	dim.pulseOn = false

	for _, c := range []struct {
		state agent.State
		name  string
	}{
		{agent.StateIdle, "idle"},
		{agent.StateBlocked, "blocked"},
		{agent.StateDone, "done"},
	} {
		_, brightCode := bright.agentMarker(c.state)
		_, dimCode := dim.agentMarker(c.state)
		if brightCode != dimCode {
			t.Errorf("agentMarker(%s) changed with pulseOn (%q vs %q), want it static", c.name, brightCode, dimCode)
		}
	}

	_, workingBright := bright.agentMarker(agent.StateWorking)
	_, workingDim := dim.agentMarker(agent.StateWorking)
	if workingBright == workingDim {
		t.Errorf("agentMarker(StateWorking) did not change with pulseOn, want the working marker to pulse")
	}
}

func TestPulsePhaseTogglesEveryHalfPeriod(t *testing.T) {
	base := time.UnixMilli(0)

	if !pulsePhase(base) {
		t.Errorf("pulsePhase(0) = false, want true")
	}
	if pulsePhase(base.Add(pulseHalfPeriod)) {
		t.Errorf("pulsePhase(base + one half-period) = true, want false")
	}
	if !pulsePhase(base.Add(2 * pulseHalfPeriod)) {
		t.Errorf("pulsePhase(base + two half-periods) = false, want true")
	}
}

func TestAgentMarkerOffTurnsItOff(t *testing.T) {
	set := markerSet{agentWorking: ""}

	if glyph, code := set.agentMarker(agent.StateWorking); glyph != "" || code != "" {
		t.Errorf("agentMarker with an empty configured glyph = (%q, %q), want empty", glyph, code)
	}
}

func TestColorizeMarkerWrapsInSGRAndResets(t *testing.T) {
	got := colorizeMarker("x", "31")

	if !strings.HasPrefix(got, "\x1b[31m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("colorizeMarker = %q, want SGR-wrapped", got)
	}

	if !strings.Contains(got, "x") {
		t.Fatalf("colorizeMarker = %q, lost the glyph", got)
	}
}

func TestColorizeMarkerNoCodeIsUnchanged(t *testing.T) {
	if got := colorizeMarker("x", ""); got != "x" {
		t.Fatalf("colorizeMarker with no code = %q, want unchanged %q", got, "x")
	}

	if got := colorizeMarker("", "31"); got != "" {
		t.Fatalf("colorizeMarker of an empty glyph = %q, want empty", got)
	}
}

// End-to-end: a session whose foreground process actually matches a
// manifest gets its gutter's last marker colorized, without pushing the
// name/status columns out of alignment — this is what the manual padding in
// sessionMarkers exists for (see gutterColumns's doc comment).
//
// The manifest is registered under every shell name testShell ("/bin/sh")
// might actually resolve to (bash, dash, sh, zsh — platform-dependent):
// pkg/gui has no access to pkg/session's unexported foregroundProcessName to
// ask directly, and pkg/session/agent_test.go already covers that the
// detected name is what Evaluate is actually called with.
func TestSessionMarkersColorizesAgentStateWithoutBreakingAlignment(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	rules := []agent.Rule{
		{State: agent.StateWorking, ScreenPattern: regexp.MustCompile("MARKER-TOKEN")},
	}
	manifests := map[string]agent.Manifest{}
	for _, name := range []string{"bash", "dash", "sh", "zsh"} {
		manifests[name] = agent.Manifest{Process: name, Rules: rules}
	}
	gui.sessions.Detector = agent.NewDetector(manifests)

	sess := newTestSession(t, gui, "agent-session")
	waitForPrompt(t, sess)

	if _, err := sess.Write([]byte("echo MARKER-TOKEN\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForScreen(t, sess, "MARKER-TOKEN")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && sess.AgentState() != agent.StateWorking {
		time.Sleep(20 * time.Millisecond)
	}
	if sess.AgentState() != agent.StateWorking {
		t.Fatalf("AgentState() = %v, want StateWorking", sess.AgentState())
	}

	set := gui.markerSet()
	gutter := sessionMarkers(sess, set, false, false)

	_, sgrCode := set.agentMarker(agent.StateWorking)
	if !strings.Contains(gutter, "\x1b["+sgrCode+"m"+set.agentWorking+"\x1b[0m") {
		t.Fatalf("gutter = %q, want the colorized working marker", gutter)
	}

	content := sessionsPanelContent([]*session.Session{sess}, set, "", nil, nil, gui.tr, 0)
	if !strings.Contains(content, sess.Name()) {
		t.Fatalf("content = %q, name column shifted out of the line", content)
	}
}
