package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
)

// The whole point of ansiColorAliases is that a name resolves to the terminal
// color that name means, not to the CSS color that happens to share it. Pinned
// against gocui's own constants: if a mapping is wrong, the UI is one shade off
// and nothing else would say so.
func TestAnsiAliasesResolveToTerminalColors(t *testing.T) {
	want := map[string]gocui.Attribute{
		"black":   gocui.ColorBlack,
		"red":     gocui.ColorRed,
		"green":   gocui.ColorGreen,
		"yellow":  gocui.ColorYellow,
		"blue":    gocui.ColorBlue,
		"magenta": gocui.ColorMagenta,
		"cyan":    gocui.ColorCyan,
		"white":   gocui.ColorWhite,
	}

	for name, attr := range want {
		if got := resolveColor(name, gocui.ColorDefault); got != attr {
			t.Errorf("resolveColor(%q) = %v, want %v", name, got, attr)
		}
	}
}

// Every bright alias must resolve to something, and to something different from
// its ordinary counterpart — otherwise the alias is decorative.
func TestBrightAliasesDifferFromOrdinary(t *testing.T) {
	for _, name := range []string{"red", "green", "yellow", "blue", "magenta", "cyan", "white", "black"} {
		ordinary := resolveColor(name, gocui.ColorDefault)
		bright := resolveColor("bright"+name, gocui.ColorDefault)

		if bright == gocui.ColorDefault {
			t.Errorf("resolveColor(%q) fell back to the default — the alias does not resolve", "bright"+name)
		}

		if bright == ordinary {
			t.Errorf("bright%s and %s resolve to the same attribute %v", name, name, ordinary)
		}
	}
}

// W3C names are not shadowed by the aliases: someone who writes "navy" or a hex
// value still gets exactly that.
func TestW3CNamesStillWork(t *testing.T) {
	if resolveColor("navy", gocui.ColorDefault) != gocui.ColorBlue {
		t.Error(`"navy" no longer resolves to the ordinary blue`)
	}

	if resolveColor("#4b8bbe", gocui.ColorDefault) == gocui.ColorDefault {
		t.Error("a hex color no longer resolves")
	}
}
