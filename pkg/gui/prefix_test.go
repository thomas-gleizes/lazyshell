package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
)

func TestPrefixFromFallsBackToDefault(t *testing.T) {
	t.Setenv("LAZYSHELL_PREFIX", "")

	if got := prefixFrom(""); got != defaultPrefixKey {
		t.Errorf("prefixFrom(\"\") = %v, want default %v", got, defaultPrefixKey)
	}
}

func TestPrefixFromUsesConfigValue(t *testing.T) {
	t.Setenv("LAZYSHELL_PREFIX", "")

	if got := prefixFrom("Ctrl+A"); got != gocui.KeyCtrlA {
		t.Errorf("prefixFrom(\"Ctrl+A\") = %v, want %v", got, gocui.KeyCtrlA)
	}
}

func TestPrefixFromEnvOverridesConfigValue(t *testing.T) {
	t.Setenv("LAZYSHELL_PREFIX", "Ctrl+G")

	if got := prefixFrom("Ctrl+A"); got != gocui.KeyCtrlG {
		t.Errorf("prefixFrom with LAZYSHELL_PREFIX set = %v, want %v (env wins)", got, gocui.KeyCtrlG)
	}
}

func TestPrefixFromFallsBackOnUnparseableConfigValue(t *testing.T) {
	t.Setenv("LAZYSHELL_PREFIX", "")

	if got := prefixFrom("not-a-real-key"); got != defaultPrefixKey {
		t.Errorf("prefixFrom with a bad config value = %v, want default %v", got, defaultPrefixKey)
	}
}
