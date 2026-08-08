package gui

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/version"
)

func TestWelcomeContentCentersItsBlock(t *testing.T) {
	hints := []welcomeHint{{key: "n", label: "Nouvelle session"}, {key: "Ctrl-C", label: "Quitter"}}

	content := welcomeContent("Aucune session ouverte.", hints, 60, 20)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		if uniseg.StringWidth(line) > 60 {
			t.Errorf("line %q is %d columns wide, want at most 60", line, uniseg.StringWidth(line))
		}
	}

	// The block is centered as a block: every non-empty line shares one
	// indent, so the key column stays a column.
	indents := map[int]bool{}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indents[len(line)-len(strings.TrimLeft(line, " "))] = true
	}

	if len(indents) != 1 {
		t.Errorf("got %d distinct indents %v, want every line aligned on one", len(indents), indents)
	}

	if indents[0] {
		t.Errorf("block starts at column 0 in a 60-column panel, want it centered:\n%s", content)
	}

	// Vertically centered too, so the first line is not glued to the frame.
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("first line = %q, want blank padding above the block", lines[0])
	}
}

func TestWelcomeContentShowsNameVersionSubtitleAndKeys(t *testing.T) {
	hints := []welcomeHint{{key: "n", label: "Nouvelle session"}}

	content := welcomeContent("Aucune session ouverte.", hints, 60, 20)

	for _, want := range []string{"lazyshell", version.Version, "Aucune session ouverte.", "n", "Nouvelle session"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q:\n%s", want, content)
		}
	}
}

// A panel too small to hold the block still shows its top rather than an
// empty frame or a negative offset that would drop the name first.
func TestWelcomeContentSurvivesATinyPanel(t *testing.T) {
	hints := []welcomeHint{{key: "n", label: "Nouvelle session"}}

	if got := welcomeContent("Aucune session.", hints, 0, 0); got != "" {
		t.Errorf("welcomeContent(0, 0) = %q, want empty", got)
	}

	content := welcomeContent("Aucune session.", hints, 12, 3)
	if !strings.Contains(content, "lazyshell") {
		t.Errorf("tiny panel content = %q, want it to start with the name", content)
	}

	for _, line := range strings.Split(content, "\n") {
		if uniseg.StringWidth(line) > 12 {
			t.Errorf("line %q overflows a 12-column panel", line)
		}
	}
}

// The advertised keys are read back out of bindings(), so a remap shows the
// user's key — the same rule the footer follows.
func TestWelcomeHintsFollowKeybindingRemap(t *testing.T) {
	cfg := config.Default()
	cfg.Keybindings = map[string]string{"new_session": "Ctrl+N"}

	gui, _ := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	hints := gui.welcomeHints()
	if len(hints) != len(welcomeActions) {
		t.Fatalf("got %d hints, want one per welcome action (%d)", len(hints), len(welcomeActions))
	}

	if hints[0].key != "Ctrl-N" {
		t.Errorf("new_session hint key = %q, want the remapped Ctrl-N", hints[0].key)
	}
}

// An active filter hiding every session is not the same state as having no
// sessions at all, and must not be described as one.
func TestWelcomeSubtitleDistinguishesFilteredFromEmpty(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if got := gui.welcomeSubtitle(); !strings.Contains(got, "Aucune session ouverte") {
		t.Errorf("subtitle with no sessions = %q", got)
	}

	newTestSession(t, gui, "shell")
	gui.filterPattern = "no-such-session"

	if got := gui.welcomeSubtitle(); !strings.Contains(got, "filtre") {
		t.Errorf("subtitle with everything filtered out = %q, want it to mention the filter", got)
	}
}

// The bug this screen exists to fix: deleting the last session left the
// previous session's render task running, so the panel kept showing its
// output. The layout pass must put the welcome screen there instead, with no
// tab strip.
func TestOutputPanelShowsWelcomeWhenNoSessionIsSelected(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	if got := view.Buffer(); !strings.Contains(got, "lazyshell") || !strings.Contains(got, "Aucune session ouverte") {
		t.Errorf("output panel with no session = %q, want the welcome screen", got)
	}

	if len(view.Tabs) != 0 {
		t.Errorf("tab strip = %v, want it dropped when there is no session to have tabs of", view.Tabs)
	}

	if got := gui.panelFooter(outputViewName, 60); got != "" {
		t.Errorf("output footer with no session = %q, want none", got)
	}
}

// And the strip comes back, with the session's own output, as soon as there
// is a session again.
func TestOutputPanelRestoresTabsOnceASessionExists(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	newTestSession(t, gui, "shell")

	if err := gui.layout(g); err != nil {
		t.Fatalf("layout: %v", err)
	}

	view, err := g.View(outputViewName)
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	if len(view.Tabs) != outputTabCount {
		t.Errorf("tab strip = %v, want %d tabs back", view.Tabs, outputTabCount)
	}
}
