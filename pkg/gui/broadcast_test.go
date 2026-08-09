package gui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// newBroadcastTestGui builds a Gui with n real sessions (broadcasting writes
// through the real pty, so a shell has to actually be there to echo it back)
// and its views laid out, ready to drive through editOutput.
func newBroadcastTestGui(t *testing.T, n int) (*Gui, *gocui.View, []*session.Session) {
	t.Helper()

	gui, g := newHeadlessGui(t)

	sessions := make([]*session.Session, n)
	for i := range n {
		sessions[i] = newTestSession(t, gui, fmt.Sprintf("s%d", i))
	}

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("output view not found: %v", err)
	}

	return gui, view, sessions
}

// selectAndToggleMark moves the selection to index and toggles its broadcast
// mark — the sequence pressing "b" on that session would produce.
func selectAndToggleMark(t *testing.T, gui *Gui, index int) {
	t.Helper()

	gui.setSelectedIndex(index)
	gui.onSelectionChanged()

	if err := gui.toggleBroadcastMark(gui.g, nil); err != nil {
		t.Fatalf("toggleBroadcastMark: %v", err)
	}
}

// waitForScreen polls sess's rendered screen until it contains want — the
// shell's own timing is not otherwise deterministic. Same polling shape as
// input_test.go's waitForSessionScreen, but against an explicit session
// rather than gui's current selection: broadcast tests need to check
// several sessions at once, not just the one on screen.
func waitForScreen(t *testing.T, sess *session.Session, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sess.Screen().Render(), want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("session %s never showed %q on screen:\n%s", sess.Name(), want, sess.Screen().Render())
}

func TestToggleBroadcastMarkTogglesMembership(t *testing.T) {
	gui, _, sessions := newBroadcastTestGui(t, 2)

	selectAndToggleMark(t, gui, 0)
	if !gui.broadcastMarks[sessions[0].ID] {
		t.Fatal("toggling once did not mark the session")
	}

	selectAndToggleMark(t, gui, 0)
	if gui.broadcastMarks[sessions[0].ID] {
		t.Error("toggling twice did not unmark the session")
	}
}

func TestToggleBroadcastMarkIsNoopWithoutASelectedSession(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if err := gui.toggleBroadcastMark(gui.g, nil); err != nil {
		t.Fatalf("toggleBroadcastMark: %v", err)
	}

	if len(gui.broadcastMarks) != 0 {
		t.Error("marking with no session selected should be a no-op")
	}
}

func TestBroadcastIsNotArmedWithOnlyOneMark(t *testing.T) {
	gui, _, sessions := newBroadcastTestGui(t, 2)

	selectAndToggleMark(t, gui, 0)

	if gui.broadcastArmed(sessions[0].ID) {
		t.Error("a single mark should not arm broadcasting")
	}
}

func TestBroadcastArmsWithTwoOrMoreMarks(t *testing.T) {
	gui, _, sessions := newBroadcastTestGui(t, 3)

	selectAndToggleMark(t, gui, 0)
	selectAndToggleMark(t, gui, 1)

	if !gui.broadcastArmed(sessions[0].ID) {
		t.Error("two marks should arm broadcasting for the first marked session")
	}
	if !gui.broadcastArmed(sessions[1].ID) {
		t.Error("two marks should arm broadcasting for the second marked session")
	}
	if gui.broadcastArmed(sessions[2].ID) {
		t.Error("an unmarked session must never read as armed")
	}
}

func TestBroadcastMarkedSessionsReturnsOnlyMarkedOnes(t *testing.T) {
	gui, _, sessions := newBroadcastTestGui(t, 3)

	selectAndToggleMark(t, gui, 0)
	selectAndToggleMark(t, gui, 2)

	marked := gui.broadcastMarkedSessions()
	if len(marked) != 2 {
		t.Fatalf("broadcastMarkedSessions() = %d sessions, want 2", len(marked))
	}

	ids := map[string]bool{marked[0].ID: true, marked[1].ID: true}
	if !ids[sessions[0].ID] || !ids[sessions[2].ID] {
		t.Errorf("broadcastMarkedSessions() = %v, want sessions 0 and 2", marked)
	}
}

func TestDispatchKeyBroadcastsOnlyToMarkedSessions(t *testing.T) {
	gui, view, sessions := newBroadcastTestGui(t, 3)

	selectAndToggleMark(t, gui, 0)
	selectAndToggleMark(t, gui, 1)

	// Attach to the first marked session and type a command through it.
	gui.setSelectedIndex(0)
	gui.onSelectionChanged()
	gui.enterPassThrough()

	typeIntoOutput(gui, view, "echo broadcast-marker\r")

	waitForScreen(t, sessions[0], "broadcast-marker")
	waitForScreen(t, sessions[1], "broadcast-marker")

	if strings.Contains(sessions[2].Screen().Render(), "broadcast-marker") {
		t.Error("the unmarked session received broadcast keystrokes it should not have")
	}
}

func TestDispatchKeyDoesNotBroadcastWhenSelectedSessionIsUnmarked(t *testing.T) {
	gui, view, sessions := newBroadcastTestGui(t, 3)

	// Two OTHER sessions are marked, but the one we attach to is not.
	selectAndToggleMark(t, gui, 1)
	selectAndToggleMark(t, gui, 2)

	gui.setSelectedIndex(0)
	gui.onSelectionChanged()
	gui.enterPassThrough()

	typeIntoOutput(gui, view, "echo solo-marker\r")

	waitForScreen(t, sessions[0], "solo-marker")

	if strings.Contains(sessions[1].Screen().Render(), "solo-marker") {
		t.Error("typing into an unmarked session must not reach the marked set")
	}
}

func TestStatusShowsBroadcastWarningWhileArmed(t *testing.T) {
	gui, _, _ := newBroadcastTestGui(t, 2)

	selectAndToggleMark(t, gui, 0)
	selectAndToggleMark(t, gui, 1)
	gui.setSelectedIndex(0)
	gui.onSelectionChanged()

	statusView, err := gui.g.View(statusViewName)
	if err != nil {
		t.Fatalf("status view not found: %v", err)
	}
	gui.renderStatus(statusView)

	if !strings.Contains(statusView.Buffer(), "2") {
		t.Errorf("status bar = %q, want it to mention the 2 marked sessions", statusView.Buffer())
	}
}

func TestStatusBroadcastWarningSurvivesPassThroughText(t *testing.T) {
	gui, _, _ := newBroadcastTestGui(t, 2)

	selectAndToggleMark(t, gui, 0)
	selectAndToggleMark(t, gui, 1)
	gui.setSelectedIndex(0)
	gui.onSelectionChanged()
	gui.enterPassThrough()

	statusView, err := gui.g.View(statusViewName)
	if err != nil {
		t.Fatalf("status view not found: %v", err)
	}
	gui.renderStatus(statusView)

	buf := statusView.Buffer()
	if !strings.Contains(buf, gui.tr.T("footer.type")) && !strings.Contains(buf, "INSERT") {
		t.Errorf("status bar = %q, want the pass-through indicator still present", buf)
	}
}

func TestSessionsFooterShowsBroadcastHint(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	got := gui.footerText(gui.sessionsFooterHints(), 200)
	if !strings.Contains(got, "b:"+gui.tr.T("footer.broadcast")) {
		t.Errorf("sessions footer = %q, want a 'b' hint for broadcast", got)
	}
}

func TestSessionsPanelGutterShowsBroadcastMarker(t *testing.T) {
	gui, _, sessions := newBroadcastTestGui(t, 2)

	selectAndToggleMark(t, gui, 0)

	got := sessionsPanelContent(sessions, gui.markerSet(), "", gui.broadcastMarks, nil, gui.tr, 0)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	const gutterWidth = 4
	if !strings.Contains(lines[0][:gutterWidth], "+") {
		t.Errorf("gutter for marked session = %q, want it to contain the broadcast marker", lines[0][:gutterWidth])
	}
	if strings.Contains(lines[1][:gutterWidth], "+") {
		t.Errorf("gutter for unmarked session = %q, want no broadcast marker", lines[1][:gutterWidth])
	}
}
