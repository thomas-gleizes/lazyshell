package gui

import (
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// Effective fills in the parts of a configuration whose real defaults live in
// this package rather than in pkg/config: an empty theme colour and a missing
// keybinding both mean "whatever the application decided", and pkg/config has
// no way to know what that is.
//
// It exists for `lazyshell config show`, whose entire promise is to report what
// is actually applied. Reporting `active_border_color: ""` for a border that is
// visibly green, or listing only the two keys someone remapped, would make the
// command answer a different question than the one it is asked.
//
// The resolution rules are the running code's own — resolveBinding for keys,
// resolveColor for colours — so what is printed cannot drift from what is drawn.
// A value the user wrote that those functions reject is replaced by the default
// they fall back to, which is the honest report: that is what the UI is using.
func Effective(cfg config.Config) config.Config {
	gui := New(nil, cfg)

	keybindings := make(map[string]string)

	for _, binding := range gui.bindings() {
		if binding.Action == "" || keybindings[binding.Action] != "" {
			continue
		}

		keybindings[binding.Action] = gui.effectiveKeySpec(binding)
	}

	cfg.Keybindings = keybindings
	cfg.Theme = config.Theme{
		ActiveBorderColor:      orDefault(cfg.Theme.ActiveBorderColor, defaultActiveBorderColorName),
		InactiveBorderColor:    orDefault(cfg.Theme.InactiveBorderColor, defaultInactiveBorderColorName),
		SelectedBgColor:        orDefault(cfg.Theme.SelectedBgColor, defaultSelectedBgColorName),
		PassThroughBorderColor: orDefault(cfg.Theme.PassThroughBorderColor, defaultPassThroughBorderColorName),
	}

	return cfg
}

// effectiveKeySpec is the key spec currently in force for a binding, written in
// the syntax a config file uses — not keyLabel's display form, which renders
// control keys as "Ctrl-Q" where gocui.Parse only accepts "Ctrl+Q". The output
// of `config show` reads as a config file, so anything in it that could be
// pasted back must actually parse.
func (gui *Gui) effectiveKeySpec(binding Binding) string {
	// A spec the user wrote is echoed verbatim when it parses: it is what is in
	// force, and reformatting someone's own text helps nobody. When it does not
	// parse, resolveBinding has already fallen back to the default, and that
	// fallback is what this must report.
	if spec, ok := gui.keymap[binding.Action]; ok {
		if _, _, err := gocui.Parse(spec); err == nil {
			return spec
		}
	}

	return keySpec(binding.Key, binding.Modifier)
}

// keySpec renders a built-in binding's key in gocui.Parse syntax.
func keySpec(key any, mod gocui.Modifier) string {
	spec := keyLabel(key, mod)

	// keyLabel is a display helper and joins modifiers with "-"; the parser
	// wants "+". Everything else it produces ("n", "Tab", "?") already round
	// trips.
	if k, ok := key.(gocui.Key); ok && k >= gocui.KeyCtrlA && k <= gocui.KeyCtrlZ {
		spec = strings.Replace(spec, "Ctrl-", "Ctrl+", 1)
	}

	return spec
}

// orDefault substitutes the default colour's name for an unset field. Filling
// the name rather than reversing the resolved gocui.Attribute back into one is
// deliberate: an attribute is an index with no unique name, so a reverse lookup
// would have to guess, and a colour the user wrote by hand ("#4b8bbe") would
// come back as something they never typed. Echoing what they wrote, and naming
// only what they left out, is the one form that is always true.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
