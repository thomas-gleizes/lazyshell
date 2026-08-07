package gui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

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
	waitForScreen(t, sess, "$")

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

	if !strings.Contains(gutter, "\x1b[33m"+set.agentWorking+"\x1b[0m") {
		t.Fatalf("gutter = %q, want the colorized working marker", gutter)
	}

	content := sessionsPanelContent([]*session.Session{sess}, set, "", nil, gui.tr)
	if !strings.Contains(content, sess.Name()) {
		t.Fatalf("content = %q, name column shifted out of the line", content)
	}
}
