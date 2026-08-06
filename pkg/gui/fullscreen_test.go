package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// newTestSession creates a session on gui's manager and returns it, failing
// the test rather than the caller having to. Sessions are cleaned up by the
// manager's Shutdown, already registered by newHeadlessGui.
func newTestSession(t testing.TB, gui *Gui, name string) *session.Session {
	t.Helper()

	sess, err := gui.sessions.New(name, "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return sess
}

// feed writes bytes straight into a session's emulator, bypassing the shell:
// these tests are about how the GUI reacts to emulator state, not about
// getting a real program to produce it.
func feed(t *testing.T, sess *session.Session, input string) {
	t.Helper()

	if _, err := sess.Screen().Write([]byte(input)); err != nil {
		t.Fatalf("Screen().Write: %v", err)
	}
}

// This is phase 6's headline defect. In OutputNormal, gocui's escape
// interpreter rejects the 256-colour SGR form and prints its body as text, so
// a themed prompt or a vim colorscheme showed "[38;5;82m" in the middle of the
// screen. In OutputTrue it is consumed and turned into a cell attribute.
func TestOutputPanelConsumes256ColourSequences(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	feed(t, sess, "\x1b[38;5;82mvert\x1b[0m")

	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	view.SetContent(buildOutputFrame(sess, 0, false, "").content)

	buffer := view.Buffer()

	if strings.Contains(buffer, "38;5") {
		t.Errorf("the 256-colour sequence was printed as text, not consumed:\n%q", buffer)
	}

	if !strings.Contains(buffer, "vert") {
		t.Errorf("the coloured text itself is missing:\n%q", buffer)
	}
}

// Truecolour is the other form pkg/screen emits and OutputNormal rejects.
func TestOutputPanelConsumesTruecolourSequences(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	feed(t, sess, "\x1b[38;2;255;128;0morange\x1b[0m")

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	view.SetContent(buildOutputFrame(sess, 0, false, "").content)

	if buffer := view.Buffer(); strings.Contains(buffer, "38;2") {
		t.Errorf("the truecolour sequence was printed as text, not consumed:\n%q", buffer)
	}
}

// The theme is resolved through gocui.GetColor / the ColorX constants, whose
// meaning depends on the output mode. Switching the app to OutputTrue must not
// have flattened them.
func TestThemeSurvivesOutputTrue(t *testing.T) {
	theme := defaultTheme()

	for name, color := range map[string]gocui.Attribute{
		"active":      theme.ActiveBorderColor,
		"selected":    theme.SelectedBgColor,
		"passThrough": theme.PassThroughBorderColor,
	} {
		if color == gocui.ColorDefault {
			t.Errorf("%s colour resolved to ColorDefault", name)
		}

		if color.Hex() < 0 {
			t.Errorf("%s colour has no RGB value in OutputTrue mode", name)
		}
	}

	if theme.ActiveBorderColor == theme.PassThroughBorderColor {
		t.Error("the pass-through border is indistinguishable from the normal active border")
	}
}

// The cursor is what makes vim and htop usable: without it you cannot see
// where you are typing. It is only drawn on the live screen, while
// pass-through is armed.
func TestOutputFrameCarriesTheCursorOnlyWhenTypedInto(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	feed(t, sess, "\x1b[5;9H")

	frame := buildOutputFrame(sess, 0, true, "")
	if !frame.cursorShown {
		t.Fatal("cursor not shown while typing into the live screen")
	}

	// CUP is 1-based, the frame is 0-based.
	if frame.cursorX != 8 || frame.cursorY != 4 {
		t.Errorf("cursor at (%d, %d), want (8, 4)", frame.cursorX, frame.cursorY)
	}

	if buildOutputFrame(sess, 0, false, "").cursorShown {
		t.Error("cursor shown while not in pass-through")
	}

	if buildOutputFrame(sess, 5, true, "").cursorShown {
		t.Error("cursor shown while scrolled back into history it is not in")
	}
}

// A full-screen application hides the cursor while it redraws; drawing it
// anyway leaves a stray block on its output.
func TestOutputFrameHonoursHiddenCursor(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	feed(t, sess, "\x1b[?25l")

	if buildOutputFrame(sess, 0, true, "").cursorShown {
		t.Error("cursor shown although the application hid it")
	}
}

// The alternate screen does not feed the scrollback, so there is nothing to
// scroll back into — and the keys that would do it belong to the application
// in control.
func TestScrollIsNeutralisedOnAlternateScreen(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")

	// Produce some real scrollback first, so the no-op cannot be explained by
	// an empty history.
	for range 60 {
		feed(t, sess, "une ligne\r\n")
	}

	gui.scrollBy(10)

	if gui.getScrollOffset() == 0 {
		t.Fatal("scrolling did nothing on the normal screen — the test proves nothing")
	}

	gui.setScrollOffset(0)
	feed(t, sess, "\x1b[?1049h")

	gui.scrollBy(10)

	if got := gui.getScrollOffset(); got != 0 {
		t.Errorf("scroll offset = %d on the alternate screen, want 0", got)
	}
}

// The status bar is the only place that says a full-screen application has the
// session — lazyshell deliberately does not switch mode on its own.
func TestStatusBarShowsAlternateScreen(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(statusViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	gui.renderStatus(view)

	if strings.Contains(view.Buffer(), altScreenIndicator) {
		t.Fatalf("alt-screen indicator shown before anything ran:\n%q", view.Buffer())
	}

	feed(t, sess, "\x1b[?1049h")
	gui.renderStatus(view)

	if !strings.Contains(view.Buffer(), altScreenIndicator) {
		t.Errorf("status bar = %q, want the %s indicator", view.Buffer(), altScreenIndicator)
	}
}

// The gutter is the only way to learn something about a session that is not
// the one on screen.
func TestSessionsPanelGutterMarkers(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	quiet := newTestSession(t, gui, "quiet")
	ringing := newTestSession(t, gui, "ringing")
	editing := newTestSession(t, gui, "editing")

	feed(t, ringing, "\a")
	feed(t, editing, "\x1b[?1049h")

	lines := strings.Split(strings.TrimRight(sessionsPanelContent([]*session.Session{quiet, ringing, editing}, testMarkers, "", nil), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want one per session:\n%q", len(lines), lines)
	}

	if strings.ContainsAny(lines[0][:2], bellMarker+altScreenMarker) {
		t.Errorf("quiet session has a marker: %q", lines[0])
	}

	if !strings.HasPrefix(lines[1], bellMarker) {
		t.Errorf("session that rang has no %s marker: %q", bellMarker, lines[1])
	}

	if !strings.HasPrefix(lines[2], altScreenMarker) {
		t.Errorf("session on the alternate screen has no %s marker: %q", altScreenMarker, lines[2])
	}
}

// A session that produced output shows the activity marker, unless it is the
// one currently selected — watching it live already counts as having seen it.
func TestSessionsPanelActivityMarker(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	quiet := newTestSession(t, gui, "quiet")
	busy := newTestSession(t, gui, "busy")
	watched := newTestSession(t, gui, "watched")

	feed(t, busy, "output\r\n")
	feed(t, watched, "output\r\n")

	content := sessionsPanelContent([]*session.Session{quiet, busy, watched}, testMarkers, watched.ID, nil)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want one per session:\n%q", len(lines), lines)
	}

	if strings.Contains(lines[0][:3], activityMarker) {
		t.Errorf("quiet session has the activity marker: %q", lines[0])
	}

	if !strings.Contains(lines[1][:3], activityMarker) {
		t.Errorf("busy session has no %s marker: %q", activityMarker, lines[1])
	}

	if strings.Contains(lines[2][:3], activityMarker) {
		t.Errorf("the selected session shows the activity marker despite being watched: %q", lines[2])
	}
}

// An exited session shows its result instead of the running/exited word: a
// checkmark for a clean exit, a cross with the code otherwise.
func TestSessionsPanelExitResult(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")

	if _, err := sess.Write([]byte("exit 7\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session never terminated")
	}

	got := sessionsPanelContent([]*session.Session{sess}, testMarkers, "", nil)
	if !strings.Contains(got, "✗ 7") {
		t.Errorf("content = %q, want the ✗ 7 exit result", got)
	}
}

// Selecting a session is what acknowledges its bell.
func TestSelectingASessionClearsItsBell(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	feed(t, sess, "\a")

	if !sess.Screen().BellPending() {
		t.Fatal("the bell was not latched")
	}

	gui.onSelectionChanged()

	if sess.Screen().BellPending() {
		t.Error("selecting the session did not clear its bell")
	}
}

// The title a shell sets is usually the running command, and is worth more in
// a session list than a working directory repeated on every line.
func TestSessionsPanelShowsTheTerminalTitle(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")

	if got := sessionsPanelContent([]*session.Session{sess}, testMarkers, "", nil); !strings.Contains(got, sess.Cwd) {
		t.Errorf("content = %q, want the cwd while no title is set", got)
	}

	feed(t, sess, "\x1b]0;vim ROADMAP.md\a")

	got := sessionsPanelContent([]*session.Session{sess}, testMarkers, "", nil)
	if !strings.Contains(got, "vim ROADMAP.md") {
		t.Errorf("content = %q, want the terminal title", got)
	}
}

// A g.Update redraws the whole screen, so a panel nobody changed must not
// schedule one — this is what stops an idle lazyshell repainting ~33 times a
// second forever.
func TestSessionsPanelSkipsUnchangedRedraws(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	newTestSession(t, gui, "s")

	content := sessionsPanelContent(gui.sessions.List(), testMarkers, "", nil)

	if !gui.sessionsPanelChanged(content, 0) {
		t.Fatal("the first render was reported as unchanged")
	}

	if gui.sessionsPanelChanged(content, 0) {
		t.Error("an identical render was reported as changed")
	}

	if !gui.sessionsPanelChanged(content, 1) {
		t.Error("a selection change was reported as unchanged")
	}

	gui.invalidateSessionsPanel()

	if !gui.sessionsPanelChanged(content, 1) {
		t.Error("an invalidated panel was reported as unchanged")
	}
}

// Same rule for the output panel, where it matters most: a session sitting at
// an idle prompt renders the identical screen 33 times a second.
func TestOutputFrameIsStableWhileNothingHappens(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	feed(t, sess, "un prompt$ ")

	first := buildOutputFrame(sess, 0, true, "")
	second := buildOutputFrame(sess, 0, true, "")

	if first != second {
		t.Error("two frames of an unchanged screen differ, so the redraw skip can never fire")
	}

	// Moving the cursor alone must still count as a change, or the cursor
	// visibly sticks while typing at a prompt that has not echoed yet.
	feed(t, sess, "\x1b[10;1H")

	if buildOutputFrame(sess, 0, true, "") == first {
		t.Error("a cursor move produced an identical frame")
	}
}

// DECCKM decides how the arrow keys must be encoded for the application in
// control; the GUI has to read it from the selected session's emulator.
func TestKeysFollowTheSessionsCursorKeyMode(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	gui.setSelectedIndex(0)

	if got := gui.translate(gocui.KeyArrowUp, 0, gocui.ModNone); string(got) != "\x1b[A" {
		t.Errorf("translate(Up) = %q in normal mode, want %q", got, "\x1b[A")
	}

	feed(t, sess, "\x1b[?1h")

	if got := gui.translate(gocui.KeyArrowUp, 0, gocui.ModNone); string(got) != "\x1bOA" {
		t.Errorf("translate(Up) = %q under DECCKM, want %q", got, "\x1bOA")
	}
}

// Typing into a shell that has exited does nothing at all, which looks exactly
// like a frozen application from the user's side.
//
// The write must be reported on the session's *status*, not on an error from
// Write: Session.Kill only closes the pty when it has to escalate to SIGKILL,
// so a shell that died cleanly on SIGTERM leaves a pty master that still
// accepts writes. An earlier version of this checked the error and passed or
// failed depending on which kill path ran — reliably on WSL2, where SIGTERM to
// a pty owner is delayed, and not at all elsewhere.
func TestWritingToAnExitedSessionIsReported(t *testing.T) {
	gui, g := newHeadlessGui(t)

	sess := newTestSession(t, gui, "s")
	gui.setSelectedIndex(0)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	if err := gui.sessions.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// drain sets the status before closing done, so waiting here guarantees
	// StatusExited below whichever kill path ran.
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session never terminated")
	}

	if sess.Status() != session.StatusExited {
		t.Fatalf("status = %v after Done(), want exited", sess.Status())
	}

	gui.writeToSelected([]byte("ls\r"))

	if gui.lastError == "" {
		t.Error("writing to a dead session reported nothing in the status bar")
	}
}
