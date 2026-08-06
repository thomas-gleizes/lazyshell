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

func TestShowHelpOpensPopupAndAnyKeyCloses(t *testing.T) {
	gui, g := newHeadlessGui(t)

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	if err := gui.showHelp(g, nil); err != nil {
		t.Fatalf("showHelp: %v", err)
	}

	view, err := g.View(helpViewName)
	if err != nil {
		t.Fatalf("help view not found after showHelp: %v", err)
	}
	if current := g.CurrentView(); current == nil || current.Name() != helpViewName {
		t.Errorf("current view = %v, want %q", current, helpViewName)
	}

	if handled := gui.closeHelpOnAnyKey(view, 0, 'x', gocui.ModNone); !handled {
		t.Error("closeHelpOnAnyKey did not report the key as handled")
	}

	if _, err := g.View(helpViewName); err == nil {
		t.Error("help view still exists after a keypress")
	}
	if current := g.CurrentView(); current == nil || current.Name() != sessionsViewName {
		t.Errorf("current view after closing help = %v, want %q", current, sessionsViewName)
	}
}
