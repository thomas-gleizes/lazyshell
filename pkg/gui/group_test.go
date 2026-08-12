package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// newGroupedTestGui builds a Gui with one session per entry in groups, named
// after its index, and returns them in creation order. A "" group means an
// ungrouped session.
func newGroupedTestGui(t *testing.T, groups ...string) (*Gui, []*session.Session) {
	t.Helper()

	gui, g := newHeadlessGui(t)

	sessions := make([]*session.Session, 0, len(groups))
	for i, group := range groups {
		sess := newTestSession(t, gui, string(rune('a'+i)))
		sess.SetGroup(group)
		sessions = append(sessions, sess)
	}

	// The views have to exist: the prompt and the click handler both go
	// through them.
	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	return gui, sessions
}

// submitPromptText types text into the open prompt and submits it, the same
// two steps a user performs — the prompt's callback is what the group tests
// are actually exercising.
func (gui *Gui) submitPromptText(t *testing.T, text string) error {
	t.Helper()

	view, err := gui.g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}

	view.Clear()
	if _, err := view.Write([]byte(text)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	return gui.submitPrompt(gui.g, view)
}

// sessionNames is the readable form of a display order, for failure messages
// and comparisons.
func sessionNames(sessions []*session.Session) []string {
	names := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		names = append(names, sess.Name())
	}

	return names
}

// Declared groups come first, in the order the project file listed them —
// never alphabetically, since that order is the file author's statement of
// what matters first. Groups that only exist at runtime follow, in order of
// first appearance.
func TestGroupOrderHonoursDeclarationThenFirstAppearance(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "zeta", "runtime-b", "alpha", "runtime-a", "alpha")

	got := groupOrderOf(gui.displaySessions(), []string{"zeta", "alpha", "never-used"})
	want := []string{"zeta", "alpha", "runtime-b", "runtime-a"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("groupOrderOf() = %v, want %v", got, want)
	}
}

// A declared group with no visible session contributes no header: the headers
// are derived from what is on screen, so a filter that hides a whole group
// must not leave an empty box behind.
func TestGroupOrderSkipsGroupsWithNoVisibleSession(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "build")

	got := groupOrderOf(gui.displaySessions(), []string{"build", "agents"})

	if len(got) != 1 || got[0] != "build" {
		t.Errorf("groupOrderOf() = %v, want only the group that has a session", got)
	}
}

// Sessions cluster by group in the declared order, ungrouped ones last, and
// the manager's own order is preserved inside each group — a session must
// never move relative to its neighbours for any reason but a group change.
func TestOrderByGroupIsStableWithinAGroupAndPutsUngroupedLast(t *testing.T) {
	//        a=api   b=(none)  c=api   d=web   e=(none)
	gui, _ := newGroupedTestGui(t, "api", "", "api", "web", "")

	got := sessionNames(orderByGroup(gui.filteredSessions(), []string{"web", "api"}))
	want := []string{"d", "a", "c", "b", "e"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("orderByGroup() = %v, want %v", got, want)
	}
}

// The no-groups case must come back untouched — it is the state every
// lazyshell without a project file is in.
func TestOrderByGroupLeavesAnUngroupedListAlone(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "", "", "")

	got := orderByGroup(gui.filteredSessions(), nil)

	if strings.Join(sessionNames(got), ",") != strings.Join(sessionNames(sessions), ",") {
		t.Errorf("orderByGroup() = %v, want the input order %v", sessionNames(got), sessionNames(sessions))
	}
}

// The regression guard for the whole row-model refactor: with nothing
// grouped, the panel renders exactly the flat list it always did — no
// headers, not even an "ungrouped" one.
func TestSessionRowsRendersNoHeaderWhenNothingIsGrouped(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "", "")

	rows := sessionRows(sessions, testMarkers, "", nil, nil, gui.tr, 40)

	if len(rows) != len(sessions) {
		t.Fatalf("sessionRows() produced %d rows for %d ungrouped sessions, want one each", len(rows), len(sessions))
	}

	for i, row := range rows {
		if row.sess == nil {
			t.Errorf("row %d is not a session's — an ungrouped list must have no header lines", i)
		}
	}
}

// One header per group, in display order, each immediately above its own
// sessions; the ungrouped tail gets one too, but only because other groups
// exist.
func TestSessionRowsInsertsOneHeaderPerVisibleGroup(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "", "api", "web")

	rows := sessionRows(orderByGroup(gui.filteredSessions(), []string{"api", "web"}),
		testMarkers, "", nil, nil, gui.tr, 40)

	// api header, a, c, web header, d, ungrouped header, b
	if len(rows) != 7 {
		t.Fatalf("sessionRows() = %d rows, want 7 (4 sessions + 3 headers)", len(rows))
	}

	for _, headerAt := range []int{0, 3, 5} {
		if rows[headerAt].sess != nil {
			t.Errorf("row %d is a session's, want a group header", headerAt)
		}
	}

	if !strings.Contains(rows[0].text, "api") {
		t.Errorf("first header = %q, want it to name the api group", rows[0].text)
	}

	if !strings.Contains(rows[5].text, gui.tr.T("sessions.group_ungrouped")) {
		t.Errorf("last header = %q, want the ungrouped label", rows[5].text)
	}
}

// Every row's text carries its own terminator, so the rendered panel has
// exactly one line per row and nothing is silently glued together.
func TestRowsTextIsOneLinePerRow(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "web")

	rows := sessionRows(orderByGroup(gui.filteredSessions(), nil), testMarkers, "", nil, nil, gui.tr, 40)

	if got := strings.Count(rowsText(rows), "\n"); got != len(rows) {
		t.Errorf("rowsText() has %d lines for %d rows, want them equal", got, len(rows))
	}
}

// The conversion a session index goes through to become a highlighted line.
// It must count past the headers — and, crucially, can never answer with a
// header's own line, which is what stops gocui's Highlight from painting one.
func TestRowLineForSessionIndexSkipsHeaders(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "web")

	rows := sessionRows(orderByGroup(gui.filteredSessions(), []string{"api", "web"}),
		testMarkers, "", nil, nil, gui.tr, 40)

	// rows: 0 header(api), 1 a, 2 header(web), 3 b
	for index, wantLine := range map[int]int{0: 1, 1: 3} {
		if got := rowLineForSessionIndex(rows, index); got != wantLine {
			t.Errorf("rowLineForSessionIndex(%d) = %d, want %d", index, got, wantLine)
		}
	}

	for _, index := range []int{-1, 2, 99} {
		if got := rowLineForSessionIndex(rows, index); got != -1 {
			t.Errorf("rowLineForSessionIndex(%d) = %d, want -1 for an index with no session", index, got)
		}
	}
}

// The inverse, which only the mouse needs: a click lands on a line, and a
// line is not always a session.
func TestSessionIndexForRowLineIsMinusOneOnAHeader(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "web")

	rows := sessionRows(orderByGroup(gui.filteredSessions(), []string{"api", "web"}),
		testMarkers, "", nil, nil, gui.tr, 40)

	for line, wantIndex := range map[int]int{0: -1, 1: 0, 2: -1, 3: 1, 4: -1, -1: -1} {
		if got := sessionIndexForRowLine(rows, line); got != wantIndex {
			t.Errorf("sessionIndexForRowLine(%d) = %d, want %d", line, got, wantIndex)
		}
	}
}

// The placeholder shown when there are no sessions is a row like any other,
// and an unselectable one — clicking it must do nothing, exactly as it did
// before the row model existed.
func TestSessionRowsPlaceholderIsNotSelectable(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	rows := sessionRows(nil, testMarkers, "", nil, nil, gui.tr, 40)

	if len(rows) != 1 || rows[0].sess != nil {
		t.Fatalf("sessionRows(nil) = %+v, want a single unselectable placeholder row", rows)
	}

	if got := sessionIndexForRowLine(rows, 0); got != -1 {
		t.Errorf("sessionIndexForRowLine on the placeholder = %d, want -1", got)
	}
}

// Walking the whole list with j must stop on a session every single time.
// This is the property the entire session-index model exists to guarantee:
// selectedIndex names a session, so there is no state in which it can be
// pointing at a header.
func TestSelectionMovedNeverLandsOnAHeader(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api", "", "web", "api", "")

	gui.setSelectedIndex(0)

	for step := range len(sessions) + 2 {
		rows := gui.panelRows()
		index := gui.getSelectedIndex()

		line := rowLineForSessionIndex(rows, index)
		if line < 0 || rows[line].sess == nil {
			t.Fatalf("step %d: selectedIndex %d maps to line %d, which is not a session's", step, index, line)
		}

		if err := gui.selectionMoved(1)(gui.g, nil); err != nil {
			t.Fatalf("selectionMoved: %v", err)
		}
	}
}

// A header names a group; it is not a thing you can be "on". Clicking one
// leaves the selection where it was.
func TestClickOnAGroupHeaderIsANoOp(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "web")

	// clickSession moves focus first, which needs the views to exist.
	for range 2 {
		if err := gui.layout(gui.g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	gui.setSelectedIndex(1)

	rows := gui.panelRows()
	if rows[0].sess != nil {
		t.Fatalf("expected line 0 to be a group header, got a session's line")
	}

	if err := gui.clickSession(gocui.ViewMouseBindingOpts{Y: 0}); err != nil {
		t.Fatalf("clickSession: %v", err)
	}

	if got := gui.getSelectedIndex(); got != 1 {
		t.Errorf("selectedIndex after clicking a header = %d, want it left at 1", got)
	}
}

// The header's rule fills the panel so the boundary reads across a narrow
// column, and never overflows it — an over-long line would wrap and cost a
// second row, breaking the one-line-per-row rule the model depends on.
func TestGroupHeaderLineFillsTheWidthWithoutOverflowing(t *testing.T) {
	line := strings.TrimSuffix(groupHeaderLine("api", 20), "\n")
	plain := strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[2m"), "\x1b[0m")

	if got := len([]rune(plain)); got != 20 {
		t.Errorf("groupHeaderLine(width 20) is %d visible characters, want 20: %q", got, plain)
	}

	long := strings.TrimSuffix(groupHeaderLine("a-very-long-group-name", 10), "\n")
	plainLong := strings.TrimSuffix(strings.TrimPrefix(long, "\x1b[2m"), "\x1b[0m")

	if !strings.Contains(plainLong, "a-very-long-group-name") {
		t.Errorf("groupHeaderLine truncated the label: %q", plainLong)
	}

	if strings.Contains(plainLong, "\n") {
		t.Errorf("groupHeaderLine produced more than one line: %q", plainLong)
	}
}

// selectGroupPickerRow finds the row satisfying match in the currently open
// picker and highlights it, the setup half of "select this row" — the test
// itself then calls triggerGroupPickerSelection, the same handler Enter runs.
func selectGroupPickerRow(t *testing.T, gui *Gui, match func(groupPickerLine) bool) {
	t.Helper()

	sess, ok := gui.sessions.Get(gui.groupPickerSessionID)
	if !ok {
		t.Fatal("selectGroupPickerRow: no picker is open")
	}

	for i, line := range gui.groupPickerLines(sess.Group()) {
		if match(line) {
			gui.groupPickerSelectedIndex = i

			return
		}
	}

	t.Fatalf("selectGroupPickerRow: no row matched")
}

// The "g" key opens a picker listing every group already in use, and
// selecting one moves the session into it — the common case, since the
// groups on screen are exactly what a user picking one wants to reuse.
// Selection stays on the session even though it just changed place in the
// display order.
func TestSetSessionGroupPicksAnExistingGroup(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api", "", "api")

	// Select the ungrouped "b", last in display order.
	gui.setSelectedIndex(2)
	if got := gui.selectedSession(); got != sessions[1] {
		t.Fatalf("selected %q, want the ungrouped b", got.Name())
	}

	if err := gui.setSessionGroup(gui.g, nil); err != nil {
		t.Fatalf("setSessionGroup: %v", err)
	}

	selectGroupPickerRow(t, gui, func(l groupPickerLine) bool {
		return l.action == pickExistingGroup && l.group == "api"
	})

	if err := gui.triggerGroupPickerSelection(gui.g, nil); err != nil {
		t.Fatalf("triggerGroupPickerSelection: %v", err)
	}

	if got := sessions[1].Group(); got != "api" {
		t.Errorf("Group() = %q, want %q", got, "api")
	}

	if got := gui.selectedSession(); got != sessions[1] {
		t.Errorf("selection followed the index instead of the session: on %q, want b", got.Name())
	}
}

// Picking "no group" is how a session leaves every group.
func TestSetSessionGroupPicksNoGroup(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api")

	if err := gui.setSessionGroup(gui.g, nil); err != nil {
		t.Fatalf("setSessionGroup: %v", err)
	}

	selectGroupPickerRow(t, gui, func(l groupPickerLine) bool { return l.action == pickNoGroup })

	if err := gui.triggerGroupPickerSelection(gui.g, nil); err != nil {
		t.Fatalf("triggerGroupPickerSelection: %v", err)
	}

	if got := sessions[0].Group(); got != "" {
		t.Errorf("Group() = %q, want the session ungrouped", got)
	}
}

// Picking "+ new group…" falls through to the plain text prompt — the escape
// hatch for a name that is not on the list yet. An empty submission there is
// still how a session ends up ungrouped, same as picking "no group" directly.
func TestSetSessionGroupPicksNewGroupThenTypes(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api", "")

	// Select the ungrouped session; only "api" and "no group" are on the list.
	gui.setSelectedIndex(1)

	if err := gui.setSessionGroup(gui.g, nil); err != nil {
		t.Fatalf("setSessionGroup: %v", err)
	}

	selectGroupPickerRow(t, gui, func(l groupPickerLine) bool { return l.action == pickNewGroup })

	if err := gui.triggerGroupPickerSelection(gui.g, nil); err != nil {
		t.Fatalf("triggerGroupPickerSelection: %v", err)
	}

	if _, err := gui.g.View(promptViewName); err != nil {
		t.Fatalf("prompt view not found after picking \"new group\": %v", err)
	}

	if err := gui.submitPromptText(t, "web"); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}

	if got := sessions[1].Group(); got != "web" {
		t.Errorf("Group() = %q, want the typed %q", got, "web")
	}
}

// With no group anywhere yet, a picker holding only "+ new group…" is not
// worth showing: "g" goes straight to the text prompt, exactly the old
// single-popup behaviour.
func TestSetSessionGroupWithNoGroupsGoesStraightToThePrompt(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "")

	if err := gui.setSessionGroup(gui.g, nil); err != nil {
		t.Fatalf("setSessionGroup: %v", err)
	}

	if _, err := gui.g.View(groupPickerViewName); err == nil {
		t.Error("the picker opened even though no group exists to pick from")
	}

	if _, err := gui.g.View(promptViewName); err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
}

// The session's current group is marked in the list, not excluded from it —
// re-picking it is a harmless no-op, and the mark is what tells the user
// where they already are.
func TestGroupPickerMarksTheCurrentGroup(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api", "web")

	gui.setSelectedIndex(0)

	lines := gui.groupPickerLines(sessions[0].Group())

	found := false

	for _, l := range lines {
		if l.action == pickExistingGroup && l.group == "api" {
			found = true

			if !strings.HasPrefix(l.text, "✓") {
				t.Errorf("current group row = %q, want it marked", l.text)
			}
		}
	}

	if !found {
		t.Fatal("the session's own group is not in its picker list")
	}
}

// "G" narrows to the selected session's group and clears on a second press.
// It composes with the text filter as an AND.
func TestToggleGroupFilterNarrowsAndClears(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "web", "api")

	gui.setSelectedIndex(0) // an "api" session

	if err := gui.toggleGroupFilter(gui.g, nil); err != nil {
		t.Fatalf("toggleGroupFilter: %v", err)
	}

	if got := len(gui.filteredSessions()); got != 2 {
		t.Errorf("filtered = %d sessions, want the 2 in the api group", got)
	}
	if !gui.filterActive() {
		t.Error("filterActive() is false with a group filter set")
	}

	// The text filter narrows further rather than replacing it. A name no
	// path component can contain: filteredSessions matches the cwd too, and
	// every test session shares the repository's own directory.
	sessions := gui.groupSessions("api")
	sessions[0].SetName("zzz-only-this-one")

	gui.filterPattern = "zzz-only"
	if got := len(gui.filteredSessions()); got != 1 {
		t.Errorf("filtered = %d sessions, want the 1 matching both filters", got)
	}
	gui.filterPattern = ""

	if err := gui.toggleGroupFilter(gui.g, nil); err != nil {
		t.Fatalf("toggleGroupFilter: %v", err)
	}

	if got := len(gui.filteredSessions()); got != 3 {
		t.Errorf("filtered = %d sessions after clearing, want all 3", got)
	}
}

// Esc clears both kinds of narrowing at once: the user thinks of them as one
// filter, and having to press it twice would be a distinction with no purpose.
func TestClearFilterClearsTheGroupFilterToo(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "web")

	gui.groupFilter = "api"
	gui.filterPattern = "a"

	if err := gui.clearFilterKey(gui.g, nil); err != nil {
		t.Fatalf("clearFilterKey: %v", err)
	}

	if gui.filterActive() {
		t.Errorf("filterActive() is still true: pattern %q, group %q", gui.filterPattern, gui.groupFilter)
	}
}

// "A" marks the whole group for broadcast, and unmarks it on a second press.
// It reuses broadcastMarks, so broadcastArmed picks it up with no knowledge
// that groups exist.
func TestToggleGroupBroadcastMarksAndUnmarksTheWholeGroup(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api", "web", "api")

	gui.setSelectedIndex(0)

	if err := gui.toggleGroupBroadcast(gui.g, nil); err != nil {
		t.Fatalf("toggleGroupBroadcast: %v", err)
	}

	if !gui.broadcastMarks[sessions[0].ID] || !gui.broadcastMarks[sessions[2].ID] {
		t.Error("the api group was not fully marked")
	}
	if gui.broadcastMarks[sessions[1].ID] {
		t.Error("the web session was marked, but it is in another group")
	}
	if !gui.broadcastArmed(sessions[0].ID) {
		t.Error("broadcastArmed is false although two sessions of the group are marked")
	}

	if err := gui.toggleGroupBroadcast(gui.g, nil); err != nil {
		t.Fatalf("toggleGroupBroadcast: %v", err)
	}

	if len(gui.broadcastMarks) != 0 {
		t.Errorf("marks = %v, want them all cleared by the second press", gui.broadcastMarks)
	}
}

// A group action reaches every member, including ones a filter is currently
// hiding: "kill the group" means the group, not the part of it on screen.
func TestGroupSessionsIgnoresAnActiveFilter(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "api", "api")

	// A pattern no path component can contain — filteredSessions matches the
	// cwd as well as the name.
	sessions[0].SetName("zzz-visible")
	gui.filterPattern = "zzz-visible"

	if got := len(gui.filteredSessions()); got != 1 {
		t.Fatalf("filter setup is wrong: %d sessions visible, want 1", got)
	}

	if got := len(gui.groupSessions("api")); got != 2 {
		t.Errorf("groupSessions() = %d, want both members despite the filter", got)
	}
}

// The four group actions are unavailable — dimmed in the help popup — while
// the selection is ungrouped, since they address the group through it.
func TestGroupActionsNeedAGroupedSelection(t *testing.T) {
	gui, sessions := newGroupedTestGui(t, "")

	if hasSelectedGroup(gui) {
		t.Error("hasSelectedGroup is true with an ungrouped selection")
	}

	sessions[0].SetGroup("api")

	if !hasSelectedGroup(gui) {
		t.Error("hasSelectedGroup is false although the selection is grouped")
	}
}

// Restarting a group skips the sessions still running rather than failing on
// them: a part-exited group is the normal case, and it is exactly when the
// key is wanted.
func TestRestartGroupReportsWhenNothingHasExited(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "api", "api")

	gui.busyInline = true

	if err := gui.restartGroup(gui.g, nil); err != nil {
		t.Fatalf("restartGroup: %v", err)
	}

	if !strings.Contains(gui.lastError, "api") {
		t.Errorf("lastError = %q, want it to name the group with nothing to restart", gui.lastError)
	}
}

// stubbornShell writes a shell script that ignores SIGTERM, so killing it
// costs a full KillTimeout before Manager.Kill escalates to SIGKILL. That
// escalation is what makes the test below measure anything.
func stubbornShell(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stubborn.sh")

	script := "#!/bin/sh\ntrap '' TERM\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

// Killing a group must cost one KillTimeout, not one per session. Found by
// driving the real binary, not by the tests: sequential kills blew
// pkg/control's 3 s client deadline on a group of *two*, so `ctl group-kill`
// reported a transport timeout for kills that had in fact succeeded — the one
// answer that API must never give.
func TestKillSessionsRunsConcurrently(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	const killTimeout = 400 * time.Millisecond

	gui.sessions.KillTimeout = killTimeout

	shell := stubbornShell(t)

	var ids []string

	for range 3 {
		sess, err := gui.sessions.NewWithOptions(session.Options{Name: "s", Shell: shell})
		if err != nil {
			t.Fatalf("NewWithOptions: %v", err)
		}

		ids = append(ids, sess.ID)
	}

	start := time.Now()

	killed, err := gui.killSessions(ids)
	if err != nil {
		t.Fatalf("killSessions: %v", err)
	}

	elapsed := time.Since(start)

	if killed != len(ids) {
		t.Errorf("killed %d sessions, want %d", killed, len(ids))
	}

	// Sequential would be 3 × killTimeout; the margin is wide enough that only
	// an actually sequential loop trips it.
	if limit := 2 * killTimeout; elapsed > limit {
		t.Errorf("killing %d sessions took %v, want under %v — the kills are not concurrent",
			len(ids), elapsed, limit)
	}
}

// A group filter says so in the status bar, and says which group. Reusing the
// text filter's wording would print an empty pattern for a narrowing the user
// very much did set, leaving them no clue why the list is short.
func TestStatusBarNamesTheGroupFilter(t *testing.T) {
	gui, _ := newGroupedTestGui(t, "services", "agents")
	gui.passThroughActive = false // New default it armed (ADR 0011); this test wants locked

	gui.setSelectedIndex(0)

	if err := gui.toggleGroupFilter(gui.g, nil); err != nil {
		t.Fatalf("toggleGroupFilter: %v", err)
	}

	view, err := gui.g.View(statusViewName)
	if err != nil {
		t.Fatalf("status view: %v", err)
	}

	gui.renderStatus(view)

	if got := view.Buffer(); !strings.Contains(got, "services") {
		t.Errorf("status bar = %q, want it to name the filtered group", got)
	}
}
