package gui

import (
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

func TestHelpContentListsEveryBindingDescription(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	content := gui.helpContent()

	for _, b := range gui.bindings() {
		if !strings.Contains(content, b.Description) {
			t.Errorf("helpContent() missing description %q", b.Description)
		}
	}
}

func TestHelpContentReflectsConfigRemap(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.keymap = map[string]string{"new_session": "N"}

	content := gui.helpContent()

	if !strings.Contains(content, "N ") && !strings.Contains(content, "N  ") {
		t.Errorf("helpContent() does not show the remapped key N:\n%s", content)
	}
}

// TestHelpLinesSplitsActionableFromUnavailable checks the core of the
// interactive popup: with no session selected, a binding gated on
// hasSelectedSession (e.g. kill_session) must land in the unavailable group,
// while an always-enabled one (e.g. new_session) must land in the available
// group.
func TestHelpLinesSplitsActionableFromUnavailable(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	lines := gui.helpLines()

	var killLine, newLine *helpLine
	for i, l := range lines {
		if !l.selectable {
			continue
		}
		switch l.binding.Action {
		case "kill_session":
			killLine = &lines[i]
		case "new_session":
			newLine = &lines[i]
		}
	}

	if killLine == nil {
		t.Fatal("kill_session binding not found in helpLines()")
	}
	if killLine.actionable {
		t.Error("kill_session should be unavailable with no session selected")
	}

	if newLine == nil {
		t.Fatal("new_session binding not found in helpLines()")
	}
	if !newLine.actionable {
		t.Error("new_session should always be actionable")
	}
}

func TestShowHelpOpensPopup(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	if _, err := g.View(helpViewName); err != nil {
		t.Fatalf("help view not found after showHelp: %v", err)
	}
	if current := g.CurrentView(); current == nil || current.Name() != helpViewName {
		t.Errorf("current view = %v, want %q", current, helpViewName)
	}
}

func TestHelpEscCloses(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	if err := gui.closeHelpKey(g, nil); err != nil {
		t.Fatalf("closeHelpKey: %v", err)
	}

	if _, err := g.View(helpViewName); err == nil {
		t.Error("help view still exists after Esc")
	}
	if current := g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view after closing help = %v, want %q", current, sessionsViewName)
	}
}

// TestHelpMoveSelectionSkipsHeaders checks that j/k only ever land the
// selection on a selectable (binding) row, never on a section header or
// blank separator.
func TestHelpMoveSelectionSkipsHeaders(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	lines := gui.helpLines()

	for range len(lines) {
		if err := gui.moveHelpSelection(1)(g, nil); err != nil {
			t.Fatalf("moveHelpSelection: %v", err)
		}

		line, _, ok := gui.currentHelpLine(gui.helpLines())
		if !ok {
			t.Fatal("currentHelpLine reported no selectable row")
		}
		if !line.selectable {
			t.Error("selection landed on a non-selectable row")
		}
	}
}

// TestHelpEnterOnUnavailableRowIsNoop checks that Enter on a row disabled by
// Binding.Enabled neither closes the popup nor invokes its Handler.
func TestHelpEnterOnUnavailableRowIsNoop(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	lines := gui.helpLines()
	idx := selectableHelpLines(lines)

	unavailableSel := -1
	for i, lineNum := range idx {
		if !lines[lineNum].actionable {
			unavailableSel = i
			break
		}
	}
	if unavailableSel == -1 {
		t.Fatal("no unavailable row found (expected kill_session etc. with no session selected)")
	}

	gui.helpSelectedIndex = unavailableSel
	if err := gui.triggerHelpSelection(g, nil); err != nil {
		t.Fatalf("triggerHelpSelection: %v", err)
	}

	if _, err := g.View(helpViewName); err != nil {
		t.Error("help popup closed on an unavailable row's Enter")
	}
}

// TestHelpEnterOnAvailableRowRunsHandlerAndCloses checks the happy path:
// Enter on an actionable row closes the popup and runs the binding's
// Handler, using new_session (always actionable, and easy to observe: it
// grows the session list by one) as the concrete binding.
func TestHelpEnterOnAvailableRowRunsHandlerAndCloses(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	before := len(gui.sessions.List())

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	lines := gui.helpLines()
	idx := selectableHelpLines(lines)

	newSessionSel := -1
	for i, lineNum := range idx {
		if lines[lineNum].binding.Action == "new_session" {
			newSessionSel = i
			break
		}
	}
	if newSessionSel == -1 {
		t.Fatal("new_session binding not found among selectable rows")
	}

	gui.helpSelectedIndex = newSessionSel
	if err := gui.triggerHelpSelection(g, nil); err != nil {
		t.Fatalf("triggerHelpSelection: %v", err)
	}

	if _, err := g.View(helpViewName); err == nil {
		t.Error("help popup still open after an available row's Enter")
	}
	if got := len(gui.sessions.List()); got != before+1 {
		t.Errorf("session count = %d, want %d (new_session handler should have run)", got, before+1)
	}
}

func TestClickHelpOnUnavailableRowJustSelects(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	lines := gui.helpLines()

	rowY := -1
	for i, l := range lines {
		if l.selectable && !l.actionable {
			rowY = i
			break
		}
	}
	if rowY == -1 {
		t.Fatal("no unavailable row found")
	}

	if err := gui.clickHelp(gocui.ViewMouseBindingOpts{Y: rowY}); err != nil {
		t.Fatalf("clickHelp: %v", err)
	}

	if _, err := g.View(helpViewName); err != nil {
		t.Error("help popup closed on a click on an unavailable row")
	}

	_, lineNum, ok := gui.currentHelpLine(gui.helpLines())
	if !ok || lineNum != rowY {
		t.Errorf("selection after click = %d, want %d", lineNum, rowY)
	}
}

func TestClickHelpOnAvailableRowRunsHandler(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	before := len(gui.sessions.List())

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	lines := gui.helpLines()

	rowY := -1
	for i, l := range lines {
		if l.selectable && l.binding.Action == "new_session" {
			rowY = i
			break
		}
	}
	if rowY == -1 {
		t.Fatal("new_session row not found")
	}

	if err := gui.clickHelp(gocui.ViewMouseBindingOpts{Y: rowY}); err != nil {
		t.Fatalf("clickHelp: %v", err)
	}

	if _, err := g.View(helpViewName); err == nil {
		t.Error("help popup still open after clicking an available row")
	}
	if got := len(gui.sessions.List()); got != before+1 {
		t.Errorf("session count = %d, want %d (new_session handler should have run)", got, before+1)
	}
}
