package gui

import (
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

// searchFor drives a full "/" search from the output view: opens the
// prompt (as typing '/' would), types pattern into it, and submits — the
// same sequence prompt_test.go uses for showPrompt directly, but reached
// through editOutput so it exercises the real keybinding wiring too.
//
// submitPrompt never propagates onSearchSubmit's error to its own return
// value — like every other popup callback, a business-logic failure (here:
// no matches) is only recorded in gui.lastError, not returned, so a
// keybinding handler failing never aborts gocui's MainLoop. Callers that
// care whether the search matched must check gui.lastError themselves.
func searchFor(t *testing.T, gui *Gui, g *gocui.Gui, pattern string) {
	t.Helper()

	if gui.showSearch(g, nil) != nil {
		t.Fatal("showSearch failed")
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

func seedScrollback(t *testing.T, gui *Gui) {
	t.Helper()

	sess := gui.selectedSession()
	// unique-anchor appears exactly once, near the top of the scrollback, so
	// a search for it is guaranteed to require an actual scroll — unlike
	// une-ligne-bavarde-1, which (as a substring) also matches -10..-199 and
	// so, at the highest of those indices, can still land on the live screen.
	// done-''marker for the same reason as input_test.go's: the pty echoes the
	// typed line, so a marker spelled literally would be matched before any
	// output exists.
	script := "echo unique-anchor; for i in $(seq 1 200); do echo une-ligne-bavarde-$i; done; echo done-''marker\n"
	if _, err := sess.Write([]byte(script)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForSessionScreen(t, gui, "done-marker")
}

func TestSlashOpensSearchPrompt(t *testing.T) {
	gui, view := newOutputTestGui(t)

	typeIntoOutput(gui, view, "/")

	if _, err := gui.g.View(promptViewName); err != nil {
		t.Fatalf("prompt view not found after '/': %v", err)
	}
}

func TestSearchWithMatchScrollsAndArmsState(t *testing.T) {
	gui, view := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "unique-anchor")

	if !gui.searchActive() {
		t.Fatal("a matched search did not arm searchActive()")
	}

	if gui.getScrollOffset() == 0 {
		t.Error("a scrollback match did not move the scroll offset")
	}

	_ = view
}

func TestSearchWithNoMatchReportsErrorAndStaysInactive(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "nothing-like-this-exists")

	if gui.lastError == "" {
		t.Error("expected a status-bar message for a pattern with no matches")
	}

	if gui.searchActive() {
		t.Error("a search with no matches should not be active")
	}
}

func TestNextMatchWrapsInBothDirections(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "une-ligne-bavarde")

	n := len(gui.searchMatches)
	if n < 2 {
		t.Fatalf("need at least 2 matches to test wrapping, got %d", n)
	}

	last := gui.searchIndex
	gui.nextMatch(1)
	if gui.searchIndex != 0 {
		t.Errorf("advancing past the last match did not wrap to 0, got %d (was %d/%d)", gui.searchIndex, last, n)
	}

	gui.nextMatch(-1)
	if gui.searchIndex != n-1 {
		t.Errorf("going back from 0 did not wrap to the last match, got %d, want %d", gui.searchIndex, n-1)
	}
}

func TestEscClearsSearch(t *testing.T) {
	gui, view := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "une-ligne-bavarde")
	if !gui.searchActive() {
		t.Fatal("search did not arm")
	}

	pressOutputKey(gui, view, gocui.KeyEsc)

	if gui.searchActive() {
		t.Error("Esc did not clear the active search")
	}
}

func TestSelectionChangeClearsSearch(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "une-ligne-bavarde")
	if !gui.searchActive() {
		t.Fatal("search did not arm")
	}

	if _, err := gui.sessions.New("second", "/bin/sh"); err != nil {
		t.Fatalf("sessions.New: %v", err)
	}
	gui.setSelectedIndex(1)
	gui.onSelectionChanged()

	if gui.searchActive() {
		t.Error("switching sessions did not clear the search")
	}
}

func TestOutputFooterShowsSearchHintByDefault(t *testing.T) {
	gui, _ := newOutputTestGui(t)

	if hints := gui.outputFooterHints(); !containsKey(hints, "/") {
		t.Errorf("outputFooterHints() = %v, want a '/' hint outside pass-through", hints)
	}
}

func TestOutputFooterShowsMatchNavigationWhileSearching(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "une-ligne-bavarde")

	hints := gui.outputFooterHints()
	if !containsKey(hints, "n/N") {
		t.Errorf("outputFooterHints() while searching = %v, want an 'n/N' hint", hints)
	}
	if !containsKey(hints, "Esc") {
		t.Errorf("outputFooterHints() while searching = %v, want an 'Esc' hint", hints)
	}
}

func containsKey(hints []footerHint, key string) bool {
	for _, h := range hints {
		if h.key == key {
			return true
		}
	}

	return false
}

func TestStatusShowsSearchMatchCount(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	seedScrollback(t, gui)

	searchFor(t, gui, gui.g, "une-ligne-bavarde")

	statusView, err := gui.g.View(statusViewName)
	if err != nil {
		t.Fatalf("status view not found: %v", err)
	}
	gui.renderStatus(statusView)

	if !strings.Contains(statusView.Buffer(), "une-ligne-bavarde") {
		t.Errorf("status bar = %q, want it to mention the active pattern", statusView.Buffer())
	}
}
