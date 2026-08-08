package gui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/debug"
)

// newTestDebugGui is a Gui with the debug mode on, writing to a temp file.
func newTestDebugGui(t *testing.T) *Gui {
	t.Helper()

	logger, err := debug.New(filepath.Join(t.TempDir(), "debug.log"))
	if err != nil {
		t.Fatalf("debug.New: %v", err)
	}

	t.Cleanup(func() { _ = logger.Close() })

	gui := &Gui{}
	gui.SetDebug(logger)

	return gui
}

// SetDebug is what --debug goes through, and showing the panel straight away
// is part of its contract: a flag whose effect you have to go and find is a
// flag that gets passed twice.
func TestSetDebugShowsThePanel(t *testing.T) {
	gui := newTestDebugGui(t)

	if gui.debug == nil {
		t.Fatal("SetDebug left gui.debug nil")
	}

	if !gui.debugPanelVisible {
		t.Error("debugPanelVisible = false after SetDebug, want true")
	}
}

func TestSetDebugNilLeavesEverythingOff(t *testing.T) {
	var gui Gui

	gui.SetDebug(nil)

	if gui.debug != nil || gui.debugPanelVisible {
		t.Errorf("SetDebug(nil) turned something on: debug=%v visible=%t", gui.debug, gui.debugPanelVisible)
	}
}

// The panel sits in the output panel's top-right corner, one cell inside its
// frame, and never overlaps it.
func TestDebugPanelGeometryAnchorsTopRight(t *testing.T) {
	// A wide, tall output panel: the preferred size fits whole.
	x0, y0, x1, y1, ok := debugPanelGeometry(40, 0, 160, 50)
	if !ok {
		t.Fatal("geometry not ok on a panel with room to spare")
	}

	if want := 159; x1 != want {
		t.Errorf("x1 = %d, want %d (one column inside the output frame)", x1, want)
	}

	if want := 1; y0 != want {
		t.Errorf("y0 = %d, want %d (one row below the output frame)", y0, want)
	}

	if got := x1 - x0 - 1; got != debugPanelWidth {
		t.Errorf("inner width = %d, want %d", got, debugPanelWidth)
	}

	if got := y1 - y0 - 1; got != debugPanelHeight {
		t.Errorf("inner height = %d, want %d", got, debugPanelHeight)
	}

	if x0 <= 40 || y1 >= 50 {
		t.Errorf("panel (%d,%d)-(%d,%d) escapes the output frame", x0, y0, x1, y1)
	}
}

// Squeezed, it shrinks rather than overflowing — up to the point where it
// stops being worth drawing at all.
func TestDebugPanelGeometryClampsToTheOutputPanel(t *testing.T) {
	x0, y0, x1, y1, ok := debugPanelGeometry(0, 0, 30, 10)
	if !ok {
		t.Fatal("geometry not ok on a panel that can still host a small one")
	}

	if got, want := x1-x0-1, 30-0-3; got != want {
		t.Errorf("inner width = %d, want the clamped %d", got, want)
	}

	if got, want := y1-y0-1, 10-0-3; got != want {
		t.Errorf("inner height = %d, want the clamped %d", got, want)
	}
}

func TestDebugPanelGeometryGivesUpWhenTooSmall(t *testing.T) {
	for _, c := range []struct {
		name           string
		x0, y0, x1, y1 int
	}{
		{"too narrow", 0, 0, 20, 40},
		{"too short", 0, 0, 100, 6},
		{"nothing at all", 0, 0, 1, 1},
	} {
		if _, _, _, _, ok := debugPanelGeometry(c.x0, c.y0, c.x1, c.y1); ok {
			t.Errorf("%s: geometry reported ok, want it dropped", c.name)
		}
	}
}

func TestDebugPanelContentIsOldestFirst(t *testing.T) {
	gui := newTestDebugGui(t)

	at := time.Date(2026, 8, 8, 14, 30, 0, 0, time.UTC)
	entries := []debug.Entry{
		{At: at, Kind: debug.KindKey, Text: "first"},
		{At: at, Kind: debug.KindAction, Text: "second"},
		{At: at, Kind: debug.KindEvent, Text: "third"},
	}

	lines := strings.Split(gui.debugPanelContent(entries, 60, 10), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), lines)
	}

	for i, want := range []string{"KEY first", "ACT second", "EVT third"} {
		if !strings.HasSuffix(lines[i], want) {
			t.Errorf("line %d = %q, want it to end with %q", i, lines[i], want)
		}
	}
}

// More entries than rows keeps the newest ones: the panel is a live tail, and
// the file has the rest.
func TestDebugPanelContentKeepsTheNewestEntries(t *testing.T) {
	gui := newTestDebugGui(t)

	entries := make([]debug.Entry, 0, 5)
	for _, text := range []string{"a", "b", "c", "d", "e"} {
		entries = append(entries, debug.Entry{Kind: debug.KindEvent, Text: text})
	}

	lines := strings.Split(gui.debugPanelContent(entries, 60, 2), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
	}

	if !strings.HasSuffix(lines[0], "d") || !strings.HasSuffix(lines[1], "e") {
		t.Errorf("kept %q, want the last two (d, e)", lines)
	}
}

// A line wider than the panel would wrap, pushing the oldest entry off the top
// of a view whose whole job is to show the last N events.
func TestDebugPanelContentTruncatesToWidth(t *testing.T) {
	gui := newTestDebugGui(t)

	entries := []debug.Entry{{Kind: debug.KindEvent, Text: strings.Repeat("x", 200)}}

	line := gui.debugPanelContent(entries, 30, 5)
	if len(line) != 30 {
		t.Errorf("line width = %d, want 30: %q", len(line), line)
	}

	if strings.Contains(line, "\n") {
		t.Errorf("truncated line still wrapped: %q", line)
	}
}

func TestDebugPanelContentEmptyState(t *testing.T) {
	gui := newTestDebugGui(t)

	if got := gui.debugPanelContent(nil, 60, 10); got == "" {
		t.Error("empty content, want the placeholder message")
	}
}

func TestTruncateToWidth(t *testing.T) {
	for _, c := range []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"much too long", 4, "much"},
		{"anything", 0, ""},
		{"anything", -1, ""},
		// Wide runes count as the columns they occupy, not as one each.
		{"日本語abc", 4, "日本"},
	} {
		if got := truncateToWidth(c.in, c.width); got != c.want {
			t.Errorf("truncateToWidth(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}

// The toggle only ever hides the panel — recording carries on, which is the
// point: you can get the output panel back without losing the trace.
func TestToggleDebugPanelKeepsRecording(t *testing.T) {
	gui := newTestDebugGui(t)

	if err := gui.toggleDebugPanel(nil, nil); err != nil {
		t.Fatalf("toggleDebugPanel: %v", err)
	}

	if gui.debugPanelVisible {
		t.Error("panel still visible after the first toggle")
	}

	if got := gui.debug.Recent(10); len(got) == 0 {
		t.Error("nothing recorded while the panel is hidden")
	}

	if err := gui.toggleDebugPanel(nil, nil); err != nil {
		t.Fatalf("toggleDebugPanel (second): %v", err)
	}

	if !gui.debugPanelVisible {
		t.Error("panel not visible again after the second toggle")
	}
}

// Registered unconditionally so bindings() stays a constant list; the handler
// is what no-ops when --debug was not given.
func TestToggleDebugPanelIsANoOpWithoutDebug(t *testing.T) {
	var gui Gui

	if err := gui.toggleDebugPanel(nil, nil); err != nil {
		t.Fatalf("toggleDebugPanel: %v", err)
	}

	if gui.debugPanelVisible {
		t.Error("panel turned visible with no logger behind it")
	}
}

func TestToggleDebugBindingIsAlwaysDeclared(t *testing.T) {
	var gui Gui

	var found *Binding

	for i, b := range gui.bindings() {
		if b.Action == "toggle_debug" {
			found = &gui.bindings()[i]

			break
		}
	}

	if found == nil {
		t.Fatal("no toggle_debug binding, but the help popup and the README doc-test both read bindings()")
	}

	if found.Key != gocui.KeyF12 {
		t.Errorf("toggle_debug key = %v, want F12", found.Key)
	}

	// Enabled is what keeps it out of the help popup while the mode is off.
	if found.Enabled == nil || found.Enabled(&gui) {
		t.Error("toggle_debug reported as enabled with no debug logger set")
	}

	withDebug := newTestDebugGui(t)
	if !found.Enabled(withDebug) {
		t.Error("toggle_debug reported as disabled while --debug is on")
	}
}

// keyLabel has to name F12, or the README doc-test compares "key(268)" against
// the documented "F12" and the build fails.
func TestKeyLabelNamesF12(t *testing.T) {
	if got := keyLabel(gocui.KeyF12, gocui.ModNone); got != "F12" {
		t.Errorf("keyLabel(F12) = %q, want F12", got)
	}
}
