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
func TestNewThemeOverridesFields(t *testing.T) {
	theme := newTheme(config.Theme{
		ActiveBorderColor:      "yellow",
		InactiveBorderColor:    "white",
		SelectedBgColor:        "cyan",
		PassThroughBorderColor: "magenta",
	})

	if want := gocui.GetColor("yellow"); theme.ActiveBorderColor != want {
		t.Errorf("ActiveBorderColor = %v, want %v", theme.ActiveBorderColor, want)
	}
	if want := gocui.GetColor("white"); theme.InactiveBorderColor != want {
		t.Errorf("InactiveBorderColor = %v, want %v", theme.InactiveBorderColor, want)
	}
	if want := gocui.GetColor("aqua"); theme.SelectedBgColor != want {
		t.Errorf("SelectedBgColor = %v, want %v (cyan aliased to aqua)", theme.SelectedBgColor, want)
	}
	if want := gocui.GetColor("fuchsia"); theme.PassThroughBorderColor != want {
		t.Errorf("PassThroughBorderColor = %v, want %v (magenta aliased to fuchsia)", theme.PassThroughBorderColor, want)
	}
}

// The two names gocui.GetColor cannot resolve on its own but every terminal
// multiplexer's config convention uses (lazygit, lazydocker, tmux).
func TestNewThemeResolvesCommonAnsiAliases(t *testing.T) {
	if got, want := resolveColor("cyan", gocui.ColorDefault), gocui.GetColor("aqua"); got != want {
		t.Errorf("resolveColor(\"cyan\", ...) = %v, want %v", got, want)
	}
	if got, want := resolveColor("magenta", gocui.ColorDefault), gocui.GetColor("fuchsia"); got != want {
		t.Errorf("resolveColor(\"magenta\", ...) = %v, want %v", got, want)
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
