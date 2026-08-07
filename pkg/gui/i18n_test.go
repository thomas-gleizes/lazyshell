package gui

import (
	"strings"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// language: en must actually change what the interface shows, not just be
// read and validated — this is the whole point of Phase 7.4. A handful of
// the most visible surfaces stand in for "the interface is translated".
func TestLanguageEnglishTranslatesTheInterface(t *testing.T) {
	cfg := config.Default()
	cfg.Language = "en"

	gui, g := newHeadlessGuiSizedWithConfig(t, 80, 24, cfg)

	if got := sessionsPanelContent(nil, testMarkers, "", nil, nil, gui.tr); !strings.Contains(got, "No sessions") {
		t.Errorf("empty panel content = %q, want the English hint", got)
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	view, err := g.View(helpViewName)
	if err != nil {
		t.Fatalf("help view not found: %v", err)
	}
	if !strings.Contains(view.Buffer(), "Quit lazyshell") {
		t.Errorf("help content = %q, want the English \"Quit lazyshell\" description", view.Buffer())
	}
	if view.Title != " help " {
		t.Errorf("help title = %q, want %q", view.Title, " help ")
	}
}

// The default config (no language set explicitly) keeps behaving exactly as
// before this package existed — no regression for every user who never
// touches the language key.
func TestDefaultLanguageStaysFrench(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if got := sessionsPanelContent(nil, testMarkers, "", nil, nil, gui.tr); !strings.Contains(got, "Aucune session") {
		t.Errorf("empty panel content = %q, want the French hint", got)
	}
}
