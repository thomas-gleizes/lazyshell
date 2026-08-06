package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

func TestNewThemeEmptyConfigKeepsDefaults(t *testing.T) {
	theme := newTheme(config.Theme{})
	def := defaultTheme()

	if theme != def {
		t.Errorf("newTheme(empty) = %+v, want defaults %+v", theme, def)
	}
}

// gocui.GetColor resolves W3C color names (tcell's own table), which are not
// the same numeric values as gocui's compact ColorRed/ColorYellow/...
// constants (a termbox-go legacy 0-7 index) — same visual color, different
// Attribute. So the assertion here is against gocui.GetColor's own output,
// not against those constants.
//
// "white" is deliberately not covered here: it is itself an ANSI name (see
// ansiColorAliases), so it resolves through the alias table, not straight
// through GetColor — that path is TestAnsiAliasesResolveToTerminalColors'
// job, in theme_alias_test.go.
func TestNewThemeOverridesFields(t *testing.T) {
	theme := newTheme(config.Theme{
		ActiveBorderColor:   "chartreuse",
		InactiveBorderColor: "orchid",
	})

	if want := gocui.GetColor("chartreuse"); theme.ActiveBorderColor != want {
		t.Errorf("ActiveBorderColor = %v, want %v", theme.ActiveBorderColor, want)
	}
	if want := gocui.GetColor("orchid"); theme.InactiveBorderColor != want {
		t.Errorf("InactiveBorderColor = %v, want %v", theme.InactiveBorderColor, want)
	}
}

// ansiColorAliases resolves the 8 ANSI names to the *ordinary* terminal color,
// not the CSS color GetColor would otherwise give: "cyan" means ANSI cyan
// (teal in CSS terms), not the CSS "cyan" (which is actually bright cyan, aka
// "aqua"). See TestAnsiAliasesResolveToTerminalColors for the full table.
func TestNewThemeResolvesCommonAnsiAliases(t *testing.T) {
	if got, want := resolveColor("cyan", gocui.ColorDefault), gocui.GetColor("teal"); got != want {
		t.Errorf("resolveColor(\"cyan\", ...) = %v, want %v (ordinary cyan, i.e. \"teal\")", got, want)
	}
	if got, want := resolveColor("magenta", gocui.ColorDefault), gocui.GetColor("purple"); got != want {
		t.Errorf("resolveColor(\"magenta\", ...) = %v, want %v (ordinary magenta, i.e. \"purple\")", got, want)
	}
}

func TestNewThemeUnparseableColorFallsBackToDefault(t *testing.T) {
	theme := newTheme(config.Theme{ActiveBorderColor: "not-a-real-color"})

	if theme.ActiveBorderColor != defaultTheme().ActiveBorderColor {
		t.Errorf("ActiveBorderColor = %v, want default %v", theme.ActiveBorderColor, defaultTheme().ActiveBorderColor)
	}
}

func TestNewThemeExplicitDefaultKeywordUsesTerminalDefault(t *testing.T) {
	theme := newTheme(config.Theme{InactiveBorderColor: "default"})

	if theme.InactiveBorderColor != gocui.ColorDefault {
		t.Errorf("InactiveBorderColor = %v, want gocui.ColorDefault", theme.InactiveBorderColor)
	}
}
