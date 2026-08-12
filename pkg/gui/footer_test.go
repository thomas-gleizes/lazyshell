package gui

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

func TestSessionsFooterTruncatesToFit(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	for _, tc := range []struct {
		width int
		want  string
	}{
		// Never a partial hint: the list stops at the first one that overflows.
		{width: 5, want: ""},
		{width: 10, want: "n:nouvelle"},
		{width: 16, want: "n:nouvelle"},
		{width: 17, want: "n:nouvelle x:tuer"},
		{width: 28, want: "n:nouvelle x:tuer"},
		{width: 29, want: "n:nouvelle x:tuer D:supprimer"},
		{width: 42, want: "n:nouvelle x:tuer D:supprimer j/k:naviguer"},
	} {
		if got := gui.panelFooter(sessionsViewName, tc.width); got != tc.want {
			t.Errorf("panelFooter(sessions, %d) = %q, want %q", tc.width, got, tc.want)
		}
	}
}

func TestFooterNeverExceedsItsWidth(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	// A footer one column too wide is not clipped by gocui, it is dropped
	// entirely (drawListFooter gives up when it would start left of x0), so
	// overflowing here means the hints silently disappear.
	for width := 1; width <= 100; width++ {
		for _, name := range []string{sessionsViewName, outputViewName} {
			if got := gui.panelFooter(name, width); uniseg.StringWidth(got) > width {
				t.Errorf("panelFooter(%s, %d) = %q, %d columns wide", name, width, got, uniseg.StringWidth(got))
			}
		}
	}
}

func TestFooterFollowsKeybindingRemap(t *testing.T) {
	cfg := config.Default()
	cfg.Keybindings = map[string]string{"new_session": "Ctrl+N"}

	gui, _ := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	got := gui.panelFooter(sessionsViewName, 40)
	if !strings.HasPrefix(got, "Ctrl-N:nouvelle") {
		t.Errorf("panelFooter = %q, want it to show the remapped Ctrl-N", got)
	}
}

// In pass-through every key but the two exits goes to the shell, so listing
// anything else would be actively wrong.
func TestOutputFooterInPassThroughShowsOnlyTheWaysOut(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	newTestSession(t, gui, "s")

	gui.passThroughActive = true

	if got := gui.panelFooter(outputViewName, 60); got != "Ctrl-O:sortir Esc Esc:sortir" {
		t.Errorf("panelFooter(output) in pass-through = %q, want both exits", got)
	}
}

func TestOutputFooterFollowsPrefixRemap(t *testing.T) {
	cfg := config.Default()
	cfg.PrefixKey = "Ctrl+A"

	gui, _ := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)
	newTestSession(t, gui, "s")

	gui.passThroughActive = true

	if got := gui.panelFooter(outputViewName, 60); got != "Ctrl-A:sortir Esc Esc:sortir" {
		t.Errorf("panelFooter(output) = %q, want Ctrl-A:sortir Esc Esc:sortir", got)
	}
}

// A prefix_key of Esc exits on a single press (editDuringPassThrough tests the
// prefix first), so the footer must not teach a double press there.
func TestOutputFooterOmitsEscPairWhenPrefixIsEsc(t *testing.T) {
	cfg := config.Default()
	cfg.PrefixKey = "Esc"

	gui, _ := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)
	newTestSession(t, gui, "s")

	gui.passThroughActive = true

	if got := gui.panelFooter(outputViewName, 60); strings.Contains(got, "Esc Esc") {
		t.Errorf("panelFooter(output) = %q, want no Esc Esc hint when the prefix is Esc", got)
	}
}

func TestOutputFooterOutsidePassThrough(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	newTestSession(t, gui, "s")
	gui.passThroughActive = false // New defaults it armed (ADR 0011); this test wants locked

	got := gui.panelFooter(outputViewName, 60)

	for _, want := range []string{"i:saisir", "PgUp/PgDn:défiler"} {
		if !strings.Contains(got, want) {
			t.Errorf("panelFooter(output) = %q, want it to contain %q", got, want)
		}
	}
}

// Scrolling does nothing while a full-screen application is in control (see
// scrollBy), so the footer must not offer it.
func TestOutputFooterDropsScrollOnAlternateScreen(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	sess := newTestSession(t, gui, "s")
	gui.passThroughActive = false // New defaults it armed (ADR 0011); this test wants locked

	feed(t, sess, "\x1b[?1049h")

	got := gui.panelFooter(outputViewName, 60)

	if strings.Contains(got, "défiler") {
		t.Errorf("panelFooter(output) = %q, want no scroll hint on the alternate screen", got)
	}
	if !strings.Contains(got, "i:saisir") {
		t.Errorf("panelFooter(output) = %q, want it to keep the pass-through hint", got)
	}
}

func TestNoFooterOnTheStatusBar(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if got := gui.panelFooter(statusViewName, 80); got != "" {
		t.Errorf("panelFooter(status) = %q, want empty", got)
	}
}

// The layout pass is what actually puts the text on the frame, and it has to
// size it from InnerWidth — Size() counts the frame columns and would produce
// a footer two columns too wide, which gocui drops without a trace.
func TestLayoutSetsPanelFooters(t *testing.T) {
	gui, g := newHeadlessGui(t)
	newTestSession(t, gui, "s")

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	for _, name := range []string{sessionsViewName, outputViewName} {
		view, err := g.View(name)
		if err != nil {
			t.Fatalf("View(%s): %v", name, err)
		}

		if view.Footer == "" {
			t.Errorf("%s has no footer after layout", name)
		}

		if uniseg.StringWidth(view.Footer) > view.InnerWidth() {
			t.Errorf("%s footer %q is %d columns wide for an inner width of %d — gocui would drop it",
				name, view.Footer, uniseg.StringWidth(view.Footer), view.InnerWidth())
		}
	}
}
