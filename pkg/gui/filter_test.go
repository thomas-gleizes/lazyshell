package gui

import (
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

// filterFor drives a full "/" filter from the sessions panel: opens the
// prompt (as pressing "/" would), types pattern into it, and submits — the
// same sequence search_test.go's searchFor uses for the scrollback search.
func filterFor(t *testing.T, gui *Gui, g *gocui.Gui, pattern string) {
	t.Helper()

	if gui.showFilter(g, nil) != nil {
		t.Fatal("showFilter failed")
	}

	view, err := g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()
	view.SetOrigin(0, 0)
	if _, err := view.Write([]byte(pattern)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := gui.submitPrompt(g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}
}

func TestFilteredSessionsWithNoPatternReturnsEverything(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if _, err := gui.sessions.New("api", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gui.sessions.New("web", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := gui.filteredSessions(); len(got) != 2 {
		t.Errorf("filteredSessions() with no pattern = %d sessions, want 2", len(got))
	}
}

func TestFilteredSessionsMatchesNameCaseInsensitively(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	api, err := gui.sessions.New("api", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gui.sessions.New("web", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	gui.filterPattern = "API"

	got := gui.filteredSessions()
	if len(got) != 1 || got[0].ID != api.ID {
		t.Errorf("filteredSessions() with pattern %q = %v, want only %s", gui.filterPattern, got, api.Name())
	}
}

func TestFilteredSessionsMatchesCwd(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	inTmp, err := gui.sessions.NewInDir("shell-a", "/bin/sh", "/tmp")
	if err != nil {
		t.Fatalf("NewInDir: %v", err)
	}
	if _, err := gui.sessions.New("shell-b", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	gui.filterPattern = "/tmp"

	got := gui.filteredSessions()
	if len(got) != 1 || got[0].ID != inTmp.ID {
		t.Errorf("filteredSessions() with pattern %q = %v, want only %s", gui.filterPattern, got, inTmp.Name())
	}
}

func TestSlashOpensFilterPrompt(t *testing.T) {
	gui, g := newPromptTestGui(t)

	if _, err := gui.sessions.New("api", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.showFilter(g, nil); err != nil {
		t.Fatalf("showFilter: %v", err)
	}

	if _, err := g.View(promptViewName); err != nil {
		t.Fatalf("prompt view not found after showFilter: %v", err)
	}
}

func TestFilterNarrowsDirectJump(t *testing.T) {
	gui, g := newPromptTestGui(t)

	if _, err := gui.sessions.New("api", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	web, err := gui.sessions.New("web", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gui.sessions.New("api-worker", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	filterFor(t, gui, g, "web")

	// Only "web" matches, so "1" must land on it even though it is the
	// second session in the manager's real (unfiltered) order.
	if err := gui.selectIndex(0)(nil, nil); err != nil {
		t.Fatalf("selectIndex(0): %v", err)
	}

	if got := gui.selectedSession(); got == nil || got.ID != web.ID {
		t.Errorf("selected = %v, want %s", got, web.Name())
	}

	// Out of range within the filtered list must still no-op silently.
	if err := gui.selectIndex(1)(nil, nil); err != nil {
		t.Fatalf("selectIndex(1): %v", err)
	}
	if got := gui.selectedSession(); got == nil || got.ID != web.ID {
		t.Errorf("out-of-range jump under a filter changed the selection: got %v", got)
	}
}

func TestFilterPreservesSelectionByID(t *testing.T) {
	gui, g := newPromptTestGui(t)

	if _, err := gui.sessions.New("api", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	web, err := gui.sessions.New("web", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	gui.setSelectedIndex(1) // "web", the second session in the real list
	gui.onSelectionChanged()

	filterFor(t, gui, g, "we")

	if got := gui.selectedSession(); got == nil || got.ID != web.ID {
		t.Errorf("filtering did not keep the previously selected session selected: got %v, want %s", got, web.Name())
	}
}

func TestClearFilterRestoresTheFullList(t *testing.T) {
	gui, g := newPromptTestGui(t)

	if _, err := gui.sessions.New("api", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gui.sessions.New("web", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	filterFor(t, gui, g, "api")
	if len(gui.filteredSessions()) != 1 {
		t.Fatalf("filter did not narrow the list")
	}

	if err := gui.clearFilterKey(g, nil); err != nil {
		t.Fatalf("clearFilterKey: %v", err)
	}

	if gui.filterActive() {
		t.Error("clearFilterKey did not clear the filter")
	}
	if got := len(gui.filteredSessions()); got != 2 {
		t.Errorf("filteredSessions() after clearing = %d, want 2", got)
	}
}

func TestNewSessionClearsAnActiveFilter(t *testing.T) {
	gui, g := newPromptTestGui(t)

	if _, err := gui.sessions.New("api", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	filterFor(t, gui, g, "api")
	if len(gui.filteredSessions()) != 1 {
		t.Fatalf("filter did not narrow the list")
	}

	if err := gui.newSession(g, nil); err != nil {
		t.Fatalf("newSession: %v", err)
	}

	if gui.filterActive() {
		t.Error("creating a session left a stale filter active, hiding the new session")
	}

	if gui.selectedSession() == nil {
		t.Fatalf("no session selected after newSession")
	}
}

func TestSessionsFooterShowsFilterHint(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	// The filter hint is an actions-based footerHint (resolved through
	// resolveBinding, not a literal key), so it must be checked against the
	// rendered text rather than containsKey, which only inspects the
	// hard-coded "key" field literal hints (like search's) carry.
	got := gui.footerText(gui.sessionsFooterHints(), 200)
	if !strings.Contains(got, "/:"+gui.tr.T("footer.filter")) {
		t.Errorf("sessions footer = %q, want a '/' hint for filtering", got)
	}
}
