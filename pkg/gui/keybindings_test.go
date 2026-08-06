package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

func TestResolveBindingWithoutActionKeepsDefault(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	b := Binding{Key: gocui.KeyCtrlC, Modifier: gocui.ModNone}
	key, mod := gui.resolveBinding(b)

	if key != gocui.KeyCtrlC || mod != gocui.ModNone {
		t.Errorf("resolveBinding = (%v, %v), want (%v, %v)", key, mod, gocui.KeyCtrlC, gocui.ModNone)
	}
}

func TestResolveBindingAppliesConfigRemap(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.keymap = map[string]string{"new_session": "N"}

	b := Binding{Action: "new_session", Key: 'n', Modifier: gocui.ModNone}
	key, _ := gui.resolveBinding(b)

	if key != 'N' {
		t.Errorf("resolveBinding with remap = %v, want 'N'", key)
	}
}

func TestResolveBindingFallsBackOnUnknownAction(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.keymap = map[string]string{"some_other_action": "N"}

	b := Binding{Action: "new_session", Key: 'n', Modifier: gocui.ModNone}
	key, _ := gui.resolveBinding(b)

	if key != 'n' {
		t.Errorf("resolveBinding with no matching entry = %v, want default 'n'", key)
	}
}

func TestResolveBindingFallsBackOnUnparseableRemap(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.keymap = map[string]string{"new_session": "not-a-real-key"}

	b := Binding{Action: "new_session", Key: 'n', Modifier: gocui.ModNone}
	key, _ := gui.resolveBinding(b)

	if key != 'n' {
		t.Errorf("resolveBinding with unparseable remap = %v, want default 'n'", key)
	}
}

// gocui.Parse only combines a modifier with a named key ("Space", "Enter"...),
// not a plain letter ("Alt+N" fails to parse and falls back to the default)
// — this pins down the case that does work.
func TestResolveBindingAppliesRemappedModifier(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.keymap = map[string]string{"new_session": "Alt+Space"}

	b := Binding{Action: "new_session", Key: 'n', Modifier: gocui.ModNone}
	key, mod := gui.resolveBinding(b)

	if key != gocui.KeySpace || mod != gocui.ModAlt {
		t.Errorf("resolveBinding(Alt+Space) = (%v, %v), want (%v, ModAlt)", key, mod, gocui.KeySpace)
	}
}

func TestSetKeybindingsHonoursConfigRemap(t *testing.T) {
	gui, g := newHeadlessGuiSizedWithConfig(t, 80, 24, config.Config{Keybindings: map[string]string{"quit": "Ctrl+Q"}})

	if err := gui.setKeybindings(g); err != nil {
		t.Fatalf("setKeybindings: %v", err)
	}

	// 'q' must no longer be bound to quit — only the remapped key is.
	handled := false
	for _, b := range gui.bindings() {
		if b.Action == "quit" {
			key, _ := gui.resolveBinding(b)
			if key == gocui.KeyCtrlQ {
				handled = true
			}
		}
	}
	if !handled {
		t.Error("quit action was not resolved to the remapped Ctrl+Q")
	}
}

func TestMatchesActionDefaultHelpKey(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if !gui.matchesAction("help", 0, '?') {
		t.Error("matchesAction(\"help\", '?') = false, want true")
	}
	if gui.matchesAction("help", 0, 'x') {
		t.Error("matchesAction(\"help\", 'x') = true, want false")
	}
}

func TestMatchesActionWithRemappedKey(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.keymap = map[string]string{"help": "Ctrl+H"}

	if !gui.matchesAction("help", gocui.KeyCtrlH, 0) {
		t.Error("matchesAction after remap to Ctrl+H = false, want true")
	}
	if gui.matchesAction("help", 0, '?') {
		t.Error("matchesAction still matches the old default '?' after remap")
	}
}
