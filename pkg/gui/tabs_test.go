package gui

import (
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

func TestSwitchTabWrapsInBothDirections(t *testing.T) {
	tests := []struct {
		name  string
		start outputTab
		delta int
		want  outputTab
	}{
		{name: "forward", start: tabTerminal, delta: 1, want: tabResources},
		{name: "forward again", start: tabResources, delta: 1, want: tabEnvironment},
		{name: "forward wraps", start: tabEnvironment, delta: 1, want: tabTerminal},
		{name: "backward", start: tabEnvironment, delta: -1, want: tabResources},
		{name: "backward wraps", start: tabTerminal, delta: -1, want: tabEnvironment},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gui, g := newHeadlessGui(t)
			if err := gui.layout(g); err != nil {
				t.Fatalf("layout: %v", err)
			}

			gui.outputTab = tc.start
			gui.switchTab(tc.delta)

			if gui.outputTab != tc.want {
				t.Errorf("outputTab = %d, want %d", gui.outputTab, tc.want)
			}

			// gocui draws the strip from the view's own TabIndex, so the two
			// have to stay in step or the highlight points at the wrong tab.
			view, err := g.View(outputViewName)
			if err != nil {
				t.Fatalf("View: %v", err)
			}

			if view.TabIndex != int(tc.want) {
				t.Errorf("view.TabIndex = %d, want %d", view.TabIndex, tc.want)
			}
		})
	}
}

// The strip must have exactly one label per tab, or gocui's click-to-index
// mapping addresses a tab that does not exist.
func TestTabLabelsCoverEveryTab(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	for _, width := range []int{0, 10, 40, 200} {
		labels := gui.tabLabels(width)
		if len(labels) != outputTabCount {
			t.Fatalf("len(tabLabels(%d)) = %d, want %d", width, len(labels), outputTabCount)
		}

		for i, label := range labels {
			if label == "" {
				t.Errorf("tabLabels(%d)[%d] is empty", width, i)
			}
		}
	}
}

// gocui truncates a strip that does not fit instead of shortening it, so a
// narrow panel would silently lose the end of its last tab.
func TestTabLabelsShrinkToFitTheStrip(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	full := gui.tabLabels(0)
	fullWidth := labelsWidth(full)

	// One column short of what the full set needs is exactly the case that
	// must fall back.
	narrow := gui.tabLabels(fullWidth - 1)

	if labelsWidth(narrow) >= fullWidth {
		t.Errorf("tabLabels(%d) is %d columns wide, want narrower than the full %d",
			fullWidth-1, labelsWidth(narrow), fullWidth)
	}

	// And with room to spare, the full labels must come back.
	if got := gui.tabLabels(fullWidth); labelsWidth(got) != fullWidth {
		t.Errorf("tabLabels(%d) fell back despite fitting exactly", fullWidth)
	}
}

// The strip drawn on the frame and the strip gocui resolves a click against
// are the same field, so a width-dependent strip must stay consistent with the
// laid-out panel.
func TestLayoutSizesTheTabStripToThePanel(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "roomy", width: 140, height: 24},
		{name: "narrow", width: 80, height: 24},
	} {
		t.Run(size.name, func(t *testing.T) {
			gui, g := newHeadlessGuiSized(t, size.width, size.height)
			if err := gui.layout(g); err != nil {
				t.Fatalf("layout: %v", err)
			}

			view, err := g.View(outputViewName)
			if err != nil {
				t.Fatalf("View: %v", err)
			}

			if got, want := labelsWidth(view.Tabs), tabStripWidth(view); got > want {
				t.Errorf("strip is %d columns wide in a %d-column panel, want it to fit", got, want)
			}
		})
	}
}

// initView is what hands the strip to gocui; without Tabs there is no strip at
// all, and without SelFgColor the active entry is drawn like the others.
func TestOutputViewCarriesTheTabStrip(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	if len(view.Tabs) != outputTabCount {
		t.Errorf("view.Tabs has %d entries, want %d", len(view.Tabs), outputTabCount)
	}

	if view.SelFgColor != gui.theme.TabActiveColor {
		t.Errorf("view.SelFgColor = %v, want the theme's TabActiveColor %v", view.SelFgColor, gui.theme.TabActiveColor)
	}

	// gocui only draws Title when Tabs is empty, so leaving both set would
	// keep a string alive that can never appear.
	if view.Title != "" {
		t.Errorf("view.Title = %q, want it empty now that the view has tabs", view.Title)
	}
}

// Leaving the output tab must not leave the keyboard pointing at a shell whose
// screen is no longer displayed.
func TestSwitchingTabDisarmsPassThrough(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.passThroughActive = true
	gui.switchTab(1)

	if gui.passThroughActive {
		t.Error("passThroughActive is still true after leaving the output tab")
	}
}

// The session's scrollback position belongs to the output tab: a detour
// through perf or env must land back exactly where it was left.
func TestSwitchingTabKeepsTheScrollOffset(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.setScrollOffset(42)

	gui.switchTab(1)
	gui.switchTab(-1)

	if got := gui.getScrollOffset(); got != 42 {
		t.Errorf("scrollOffset = %d after a round trip through another tab, want 42", got)
	}
}

func TestClickOutputTabSelectsTheClickedTab(t *testing.T) {
	tests := []struct {
		name  string
		index int
		want  outputTab
	}{
		{name: "perf", index: int(tabResources), want: tabResources},
		{name: "env", index: int(tabEnvironment), want: tabEnvironment},
		// gocui hands over whatever GetClickedTabIndex resolved; an index past
		// the strip must not address a tab that does not exist.
		{name: "out of range", index: 99, want: tabTerminal},
		{name: "negative", index: -1, want: tabTerminal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gui, g := newHeadlessGui(t)
			if err := gui.layout(g); err != nil {
				t.Fatalf("layout: %v", err)
			}

			if err := gui.clickOutputTab(tc.index); err != nil {
				t.Fatalf("clickOutputTab: %v", err)
			}

			if gui.outputTab != tc.want {
				t.Errorf("outputTab = %d, want %d", gui.outputTab, tc.want)
			}
		})
	}
}

// Everything that would act on the emulated screen has to be inert while the
// panel is showing a report instead.
func TestSecondaryTabsRefuseTheScreenKeys(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	gui.outputTab = tabEnvironment

	for _, key := range []struct {
		name string
		key  gocui.Key
		ch   rune
	}{
		{name: "i (type)", ch: 'i'},
		{name: "Enter (type)", key: gocui.KeyEnter},
		{name: "v (copy-mode)", ch: 'v'},
		{name: "/ (search)", ch: '/'},
	} {
		t.Run(key.name, func(t *testing.T) {
			if gui.editDuringScroll(view, key.key, key.ch) {
				t.Errorf("%s was claimed on a secondary tab, want it to fall through", key.name)
			}

			if gui.passThroughActive {
				t.Error("pass-through got armed from a secondary tab")
			}

			if gui.copyModeActive {
				t.Error("copy-mode got entered from a secondary tab")
			}

			if gui.searchActive() {
				t.Error("a search got started from a secondary tab")
			}
		})
	}
}

// The tab keys have to work from the output view too, which the Editor owns
// entirely — a SetKeybinding on that view is never consulted.
func TestTabKeysWorkFromTheOutputView(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	if !gui.editDuringScroll(view, 0, ']') {
		t.Fatal("']' was not claimed on the output tab")
	}

	if gui.outputTab != tabResources {
		t.Fatalf("outputTab = %d after ']', want %d", gui.outputTab, tabResources)
	}

	// And back, this time through the secondary tab's own handler.
	if !gui.editDuringScroll(view, 0, '[') {
		t.Fatal("'[' was not claimed on a secondary tab")
	}

	if gui.outputTab != tabTerminal {
		t.Errorf("outputTab = %d after '[', want %d", gui.outputTab, tabTerminal)
	}
}

// The two offsets are separate on purpose: scrolling a report must not move
// where the session's scrollback is sitting.
func TestScrollingASecondaryTabLeavesTheScrollbackAlone(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.setScrollOffset(7)
	gui.outputTab = tabEnvironment
	fillOutputView(t, g, 100)

	gui.scrollBy(-10)

	if got := gui.getScrollOffset(); got != 7 {
		t.Errorf("scrollOffset = %d, want it untouched at 7", got)
	}

	if gui.tabOffset == 0 {
		t.Error("tabOffset did not move, want the tab's own content scrolled")
	}
}

func TestSecondaryTabScrolling(t *testing.T) {
	const lines = 100

	// wantMax stands for "however far this view can actually scroll" — derived
	// from the laid-out view rather than hardcoded, since the panel's inner
	// height depends on the frame and the status bar.
	const wantMax = -1

	tests := []struct {
		name  string
		start int
		delta int
		want  int
	}{
		// scrollBy's delta counts backwards from a live bottom; a view origin
		// counts forwards, so the two have opposite signs.
		{name: "PgDn moves down", start: 0, delta: -10, want: 10},
		{name: "PgUp moves back up", start: 30, delta: 10, want: 20},
		{name: "cannot go above the top", start: 3, delta: 50, want: 0},
		{name: "cannot scroll past the last line", start: 0, delta: -1000, want: wantMax},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gui, g := newHeadlessGui(t)
			if err := gui.layout(g); err != nil {
				t.Fatalf("layout: %v", err)
			}

			gui.outputTab = tabEnvironment
			gui.tabOffset = tc.start
			fillOutputView(t, g, lines)

			view, err := g.View(outputViewName)
			if err != nil {
				t.Fatalf("View: %v", err)
			}

			want := tc.want
			if want == wantMax {
				want = maxTabOffset(view)
				if want <= 0 {
					t.Fatalf("maxTabOffset = %d, want the content to overflow the panel", want)
				}
			}

			gui.scrollBy(tc.delta)

			if gui.tabOffset != want {
				t.Errorf("tabOffset = %d, want %d", gui.tabOffset, want)
			}
		})
	}
}

// Switching tabs starts the new one at the top: an offset carried over from
// another tab's content addresses nothing in this one.
func TestSwitchingTabResetsTheTabOffset(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.outputTab = tabEnvironment
	gui.tabOffset = 40

	gui.switchTab(1)

	if gui.tabOffset != 0 {
		t.Errorf("tabOffset = %d after a tab switch, want 0", gui.tabOffset)
	}
}

// The output tab's view must always be drawn from its own origin: the emulator
// decides what is on screen, and drawCursor relies on view rows being emulator
// rows.
func TestOutputTabIsPinnedToTheTopEvenAfterScrollingAnother(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	view.SetOrigin(0, 30)
	applyTabOrigin(view, tabTerminal, 30)

	if _, y := view.Origin(); y != 0 {
		t.Errorf("origin y = %d on the output tab, want 0", y)
	}
}

// fillOutputView puts n lines into the output view, so scroll clamping has
// real content to clamp against.
func fillOutputView(t *testing.T, g *gocui.Gui, n int) {
	t.Helper()

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line"
	}

	view.SetContent(strings.Join(lines, "\n"))
}

func TestSecondaryTabFooterOffersTheTabKeys(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	gui.outputTab = tabEnvironment

	hints := gui.outputFooterHints()
	if len(hints) == 0 {
		t.Fatal("outputFooterHints() is empty on a secondary tab")
	}

	// The footer must not advertise keys that do nothing here.
	for _, hint := range hints {
		if hint.key == "i" || hint.key == "/" || hint.key == "v" {
			t.Errorf("secondary tab footer offers %q, which does nothing there", hint.key)
		}
	}
}
