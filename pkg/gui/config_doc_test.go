package gui

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// readmePath is the documentation this package's defaults are published in.
const readmePath = "../../README.md"

// readmeExample is the config.Config the README's example block parses to. The
// block is advertised as "every option at its default value", and pkg/config's
// doc_test.go already checks that for every field whose default it owns. The
// two it cannot check are keybindings and theme: Config's own defaults for both
// are empty ("keep whatever pkg/gui decided"), so the real values only exist
// here — which is exactly why they are the two most likely to go stale.
func readmeExample(t *testing.T) config.Config {
	t.Helper()

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading %s: %v", readmePath, err)
	}

	_, after, found := strings.Cut(string(data), "### Example")
	if !found {
		t.Fatal("README has no \"### Example\" section")
	}

	_, after, found = strings.Cut(after, "```yaml\n")
	if !found {
		t.Fatal("README's example section has no ```yaml block")
	}

	block, _, found := strings.Cut(after, "```")
	if !found {
		t.Fatal("unterminated ```yaml block in the README")
	}

	var cfg config.Config
	if err := yaml.Unmarshal([]byte(block), &cfg); err != nil {
		t.Fatalf("parsing the README example: %v", err)
	}

	return cfg
}

// TestREADMEKeybindingsMatchDefaults fails if a documented key is not the one
// the application actually binds. A README that says `quit: "q"` while the code
// binds something else is worse than no README: the user copies it, changes
// nothing, and concludes remapping does not work.
func TestREADMEKeybindingsMatchDefaults(t *testing.T) {
	documented := readmeExample(t).Keybindings

	var gui Gui

	seen := make(map[string]bool)

	for _, binding := range gui.bindings() {
		if binding.Action == "" || seen[binding.Action] {
			continue
		}

		seen[binding.Action] = true

		want := keyLabel(binding.Key, binding.Modifier)

		got, ok := documented[binding.Action]
		if !ok {
			t.Errorf("action %q is remappable but absent from the README's keybindings example", binding.Action)

			continue
		}

		if got != want {
			t.Errorf("README documents %s: %q, but the default binding is %q", binding.Action, got, want)
		}
	}

	for action := range documented {
		if !seen[action] {
			t.Errorf("README documents keybinding %q, which is not a remappable action", action)
		}
	}
}

// TestREADMEThemeMatchesDefaults is the same check for colors: every documented
// value must resolve to the attribute the application draws with by default.
// Compared through resolveColor rather than by name, so "default" and the
// cyan/magenta aliases are handled the same way the running code handles them.
func TestREADMEThemeMatchesDefaults(t *testing.T) {
	documented := newTheme(readmeExample(t).Theme)

	if documented != defaultTheme() {
		t.Errorf("README's theme example resolves to %+v, want the built-in defaults %+v",
			documented, defaultTheme())
	}
}
