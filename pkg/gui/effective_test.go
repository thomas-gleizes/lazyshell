package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// Effective must report what is drawn, not what was written: an unset theme
// colour is not "no colour", it is the default one, and an unremapped action is
// not unbound.
func TestEffectiveFillsInDefaults(t *testing.T) {
	got := Effective(config.Default())

	if got.Theme.ActiveBorderColor != defaultActiveBorderColorName {
		t.Errorf("Theme.ActiveBorderColor = %q, want %q", got.Theme.ActiveBorderColor, defaultActiveBorderColorName)
	}

	if got.Theme.SelectedBgColor != defaultSelectedBgColorName {
		t.Errorf("Theme.SelectedBgColor = %q, want %q", got.Theme.SelectedBgColor, defaultSelectedBgColorName)
	}

	var gui Gui

	for _, binding := range gui.bindings() {
		if binding.Action == "" {
			continue
		}

		if got.Keybindings[binding.Action] == "" {
			t.Errorf("action %q is missing from the effective keybindings", binding.Action)
		}
	}
}

// A value the user wrote is theirs: echoed as-is, never reformatted into
// something they would not recognise.
func TestEffectiveEchoesUserValues(t *testing.T) {
	cfg := config.Default()
	cfg.Theme.ActiveBorderColor = "#4b8bbe"
	cfg.Keybindings = map[string]string{"quit": "Ctrl+Q"}

	got := Effective(cfg)

	if got.Theme.ActiveBorderColor != "#4b8bbe" {
		t.Errorf("Theme.ActiveBorderColor = %q, want the hex the user wrote", got.Theme.ActiveBorderColor)
	}

	if got.Keybindings["quit"] != "Ctrl+Q" {
		t.Errorf("Keybindings[quit] = %q, want %q", got.Keybindings["quit"], "Ctrl+Q")
	}
}

// An unparseable remap is not in force — the default is — so that is what must
// be reported. Reporting the broken value back would tell the user their key
// works.
func TestEffectiveReportsTheFallbackForABrokenRemap(t *testing.T) {
	cfg := config.Default()
	cfg.Keybindings = map[string]string{"quit": "Ctrl+Nope"}

	if got := Effective(cfg).Keybindings["quit"]; got != "q" {
		t.Errorf("Keybindings[quit] = %q, want the default %q that is actually in force", got, "q")
	}
}

// Everything Effective prints reads as a config file, so all of it must parse
// back — including the control keys, which keyLabel renders as "Ctrl-Q" while
// gocui.Parse only accepts "Ctrl+Q".
func TestEffectiveKeybindingsParseBack(t *testing.T) {
	cfg := config.Default()
	cfg.Keybindings = map[string]string{"quit": "Ctrl+Q", "help": "Alt+h"}

	for action, spec := range Effective(cfg).Keybindings {
		if _, _, err := gocui.Parse(spec); err != nil {
			t.Errorf("effective %s = %q does not parse back (%v)", action, spec, err)
		}
	}
}

// The round trip that matters most: feed Effective's own output back in and
// nothing may change. A report that is not a fixed point is a report that
// reformats, and a user pasting it into their config would drift.
func TestEffectiveIsStable(t *testing.T) {
	once := Effective(config.Default())
	twice := Effective(once)

	if once.Theme != twice.Theme {
		t.Errorf("theme changed on a second pass: %+v then %+v", once.Theme, twice.Theme)
	}

	for action, spec := range once.Keybindings {
		if twice.Keybindings[action] != spec {
			t.Errorf("%s changed on a second pass: %q then %q", action, spec, twice.Keybindings[action])
		}
	}
}
