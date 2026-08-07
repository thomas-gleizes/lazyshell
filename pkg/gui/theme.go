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
	// TabActiveColor is the output panel's selected tab (pkg/gui/tabs.go).
	// gocui draws it from the view's SelFgColor, whose zero value is
	// ColorDefault — i.e. identical to the inactive tabs, which would leave
	// the strip with no way to tell which tab is showing.
	TabActiveColor gocui.Attribute
}

// The default colors' names, in the syntax a config file uses. They exist
// alongside the attributes below because a gocui.Attribute cannot be turned
// back into a name — it is a palette index — and `lazyshell config show` has to
// tell the user what to write, not what the palette holds.
//
// They are ANSI names, resolved through ansiColorAliases like any other value.
// TestREADMEThemeMatchesDefaults and TestAnsiAliasesResolveToTerminalColors are
// what keep these names and the attributes below in agreement.
const (
	defaultActiveBorderColorName      = "green"
	defaultInactiveBorderColorName    = "default"
	defaultSelectedBgColorName        = "blue"
	defaultPassThroughBorderColorName = "red"
	defaultTabActiveColorName         = "green"
)

// defaultTheme is what lazyshell draws with when nothing in the config file
// overrides it — the same colors phase 3/4 hardcoded.
func defaultTheme() Theme {
	return Theme{
		ActiveBorderColor:      gocui.ColorGreen,
		InactiveBorderColor:    gocui.ColorDefault,
		SelectedBgColor:        gocui.ColorBlue,
		PassThroughBorderColor: gocui.ColorRed,
		TabActiveColor:         gocui.ColorGreen,
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
		TabActiveColor:         resolveColor(cfg.TabActiveColor, def.TabActiveColor),
	}
}

// ansiColorAliases maps the terminal color names people actually write to the
// W3C names gocui.GetColor understands. lazyshell's config talks in ANSI terms
// — that is what a terminal UI is drawn in, and what every comparable config
// (lazygit's, lazydocker's, tmux's) uses — while GetColor's table is the CSS
// one, where the names disagree twice over:
//
//   - four ANSI names are simply absent ("cyan", "magenta", and the bright
//     variants of most colors);
//   - four more exist but mean the *bright* variant. "blue" is #0000FF, which
//     is ANSI 12, not ANSI 4; the ordinary blue is called "navy". Same for
//     "red" vs "maroon", "green" (W3C #008000, which happens to agree) and
//     "white" vs "silver".
//
// The second kind is the dangerous one: it resolves, so it silently gives a
// color one shade off from the one asked for. Mapping the whole ANSI set here,
// rather than only the names that fail outright, is what makes
// `selected_bg_color: blue` mean the blue a terminal user means.
//
// The W3C names still work unaliased for anyone who wants them: they fall
// through to GetColor untouched.
var ansiColorAliases = map[string]string{
	"black":   "black",
	"red":     "maroon",
	"green":   "green",
	"yellow":  "olive",
	"blue":    "navy",
	"magenta": "purple",
	"cyan":    "teal",
	"white":   "silver",

	"brightblack":   "gray",
	"brightred":     "red",
	"brightgreen":   "lime",
	"brightyellow":  "yellow",
	"brightblue":    "blue",
	"brightmagenta": "fuchsia",
	"brightcyan":    "aqua",
	"brightwhite":   "white",
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
