package gui

import (
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// Theme holds every color lazyshell draws chrome with, resolved to gocui
// Attributes once at startup (pkg/config's Theme is still plain strings, so
// it stays free of a gocui dependency).
type Theme struct {
	// ActiveBorderColor is the current view's frame color (lazydocker's
	// model: g.Highlight draws SelFrameColor on whichever view is current).
	ActiveBorderColor gocui.Attribute
	// InactiveBorderColor is every other view's frame color.
	InactiveBorderColor gocui.Attribute
	// SelectedBgColor is the sessions panel's highlighted-line background.
	SelectedBgColor gocui.Attribute
	// PassThroughBorderColor replaces ActiveBorderColor on the current view
	// while the output panel is in pass-through mode — the roadmap's second
	// mode indicator, alongside the status bar text.
	PassThroughBorderColor gocui.Attribute
}

// defaultTheme is what lazyshell draws with when nothing in the config file
// overrides it — the same colors phase 3/4 hardcoded.
func defaultTheme() Theme {
	return Theme{
		ActiveBorderColor:      gocui.ColorGreen,
		InactiveBorderColor:    gocui.ColorDefault,
		SelectedBgColor:        gocui.ColorBlue,
		PassThroughBorderColor: gocui.ColorRed,
	}
}

// newTheme resolves pkg/config's Theme (color names/hex strings) against
// defaultTheme, field by field: an empty or unparseable value keeps the
// built-in default rather than silently falling through to the terminal's
// own default color, so a typo in the config file degrades gracefully
// instead of making the UI monochrome.
func newTheme(cfg config.Theme) Theme {
	def := defaultTheme()

	return Theme{
		ActiveBorderColor:      resolveColor(cfg.ActiveBorderColor, def.ActiveBorderColor),
		InactiveBorderColor:    resolveColor(cfg.InactiveBorderColor, def.InactiveBorderColor),
		SelectedBgColor:        resolveColor(cfg.SelectedBgColor, def.SelectedBgColor),
		PassThroughBorderColor: resolveColor(cfg.PassThroughBorderColor, def.PassThroughBorderColor),
	}
}

// ansiColorAliases covers the two common ANSI/terminal color names that do
// not match gocui.GetColor's underlying W3C table: it knows "aqua" and
// "fuchsia", not the "cyan"/"magenta" every terminal multiplexer's own config
// (lazygit's, lazydocker's, tmux's) uses instead.
var ansiColorAliases = map[string]string{
	"cyan":    "aqua",
	"magenta": "fuchsia",
}

// resolveColor parses name (a W3C color name or "#rrggbb", gocui.GetColor's
// syntax, plus ansiColorAliases) and falls back to fallback when name is
// empty or unrecognized. The literal name "default" is the one way to
// deliberately ask for the terminal's own default color instead of falling
// back.
func resolveColor(name string, fallback gocui.Attribute) gocui.Attribute {
	if name == "" {
		return fallback
	}

	if name == "default" {
		return gocui.ColorDefault
	}

	if alias, ok := ansiColorAliases[name]; ok {
		name = alias
	}

	color := gocui.GetColor(name)
	if color == gocui.ColorDefault {
		return fallback
	}

	return color
}
