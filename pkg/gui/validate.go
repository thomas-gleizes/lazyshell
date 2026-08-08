package gui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// ValidateConfig reports everything wrong with the parts of the configuration
// that only this package can check: key specs and colors both need gocui to
// parse, and pkg/config deliberately has no gocui dependency (see its Theme,
// kept as plain strings for exactly that reason).
//
// It reports, it does not repair: every consumer here already falls back to its
// built-in default on an unparseable value — resolveBinding keeps the default
// key, resolveColor keeps the default color, prefixFrom keeps Ctrl-O. That
// graceful degradation is precisely the problem this function exists for: it is
// silent, so the user sees a keybinding that simply does not take and has no way
// to learn why.
//
// Called from pkg/app before gocui takes the terminal, so the messages land on
// a terminal that can still show them.
func ValidateConfig(cfg config.Config) []error {
	var errs []error

	if cfg.PrefixKey != "" {
		if _, _, err := gocui.Parse(cfg.PrefixKey); err != nil {
			errs = append(errs, fmt.Errorf("prefix_key %q illisible (%v), retour à %s",
				cfg.PrefixKey, err, prefixName(defaultPrefixKey)))
		} else if _, ok := parsePrefixKey(cfg.PrefixKey); !ok {
			// Parseable but not a gocui.Key: a plain rune like "a" would leave
			// pass-through with no way out, since every printable key goes to
			// the shell.
			errs = append(errs, fmt.Errorf("prefix_key %q doit être une touche de contrôle (ex. Ctrl+O), retour à %s",
				cfg.PrefixKey, prefixName(defaultPrefixKey)))
		}
	}

	errs = append(errs, validateKeybindings(cfg.Keybindings)...)
	errs = append(errs, validateTheme(cfg.Theme)...)

	return errs
}

// ParseKey exposes gocui's key-spec parser to packages that must not import
// gocui themselves — pkg/app checking $LAZYSHELL_PREFIX before reporting it as
// effective. Same signature as gocui.Parse, on purpose.
func ParseKey(spec string) (any, gocui.Modifier, error) {
	return gocui.Parse(spec)
}

// validateKeybindings checks each remap twice: that the action id exists at all
// (a `new_sesion:` typo binds nothing and warns about nothing today), and that
// the key spec parses.
func validateKeybindings(keymap map[string]string) []error {
	if len(keymap) == 0 {
		return nil
	}

	known := knownActions()

	// Sorted, so two runs over the same file produce the same messages in the
	// same order — map iteration would not.
	actions := make([]string, 0, len(keymap))
	for action := range keymap {
		actions = append(actions, action)
	}

	sort.Strings(actions)

	var errs []error

	for _, action := range actions {
		if !known[action] {
			errs = append(errs, fmt.Errorf("keybindings: action inconnue %q (connues : %s)",
				action, strings.Join(sortedKeys(known), ", ")))

			continue
		}

		if _, _, err := gocui.Parse(keymap[action]); err != nil {
			errs = append(errs, fmt.Errorf("keybindings.%s: touche %q illisible (%v), la touche par défaut est conservée",
				action, keymap[action], err))
		}
	}

	return errs
}

// knownActions is the set of ids bindings() answers to — the same source
// resolveBinding consults, so the two can never disagree about what is
// remappable.
func knownActions() map[string]bool {
	// Zero-value Gui: bindings() only reads the Handler closures' receiver, and
	// nothing here calls them.
	var gui Gui

	actions := make(map[string]bool)

	for _, binding := range gui.bindings() {
		if binding.Action != "" {
			actions[binding.Action] = true
		}
	}

	return actions
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

// validateTheme reports the color values resolveColor would silently fall back
// on. It mirrors resolveColor's own rules exactly — empty and "default" are
// both legitimate — rather than re-deriving them.
func validateTheme(theme config.Theme) []error {
	fields := []struct {
		name  string
		value string
	}{
		{"active_border_color", theme.ActiveBorderColor},
		{"inactive_border_color", theme.InactiveBorderColor},
		{"selected_bg_color", theme.SelectedBgColor},
		{"pass_through_border_color", theme.PassThroughBorderColor},
	}

	var errs []error

	for _, field := range fields {
		if field.value == "" || field.value == "default" {
			continue
		}

		name := field.value
		if alias, ok := ansiColorAliases[name]; ok {
			name = alias
		}

		if gocui.GetColor(name) == gocui.ColorDefault {
			errs = append(errs, fmt.Errorf("theme.%s: couleur %q inconnue, la couleur par défaut est conservée",
				field.name, field.value))
		}
	}

	return errs
}
