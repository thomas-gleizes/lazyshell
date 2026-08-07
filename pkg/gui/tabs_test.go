package gui

import (
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
		{name: "forward", start: tabOutput, delta: 1, want: tabPerf},
		{name: "forward again", start: tabPerf, delta: 1, want: tabEnv},
		{name: "forward wraps", start: tabEnv, delta: 1, want: tabOutput},
		{name: "backward", start: tabEnv, delta: -1, want: tabPerf},
		{name: "backward wraps", start: tabOutput, delta: -1, want: tabEnv},
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

	labels := gui.tabLabels()
	if len(labels) != outputTabCount {
		t.Fatalf("len(tabLabels()) = %d, want %d", len(labels), outputTabCount)
	}

	for i, label := range labels {
		if label == "" {
			t.Errorf("tabLabels()[%d] is empty", i)
		}
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
		{name: "perf", index: int(tabPerf), want: tabPerf},
		{name: "env", index: int(tabEnv), want: tabEnv},
		// gocui hands over whatever GetClickedTabIndex resolved; an index past
		// the strip must not address a tab that does not exist.
		{name: "out of range", index: 99, want: tabOutput},
		{name: "negative", index: -1, want: tabOutput},
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

	gui.outputTab = tabEnv

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

	if gui.outputTab != tabPerf {
		t.Fatalf("outputTab = %d after ']', want %d", gui.outputTab, tabPerf)
	}

	// And back, this time through the secondary tab's own handler.
	if !gui.editDuringScroll(view, 0, '[') {
		t.Fatal("'[' was not claimed on a secondary tab")
	}

	if gui.outputTab != tabOutput {
		t.Errorf("outputTab = %d after '[', want %d", gui.outputTab, tabOutput)
	}
}

// scrollBy addresses the session's scrollback, which the secondary tabs are
// not showing — moving it from there would scroll something invisible.
func TestScrollingIsInertOnSecondaryTabs(t *testing.T) {
	gui, g := newHeadlessGui(t)
	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	gui.setScrollOffset(7)
	gui.outputTab = tabPerf

	gui.scrollBy(10)

	if got := gui.getScrollOffset(); got != 7 {
		t.Errorf("scrollOffset = %d, want it untouched at 7", got)
	}
}

func TestSecondaryTabFooterOffersTheTabKeys(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	gui.outputTab = tabEnv

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
