package gui

import (
	"sort"
	"strings"
)

// secretNameFragments and the "_KEY" suffix below are the heuristic behind
// env_tab.mask_secrets: the panel is as shareable as a screenshot of it, and an
// API key sitting in a session's environment must not be the thing that makes a
// screenshot dangerous.
//
// The test is on the variable's *name*, never its value — a value-based guess
// ("looks like a token") would both miss short secrets and hide innocuous
// paths. Deliberately broad: a masked variable is a small annoyance, a leaked
// one is not, and the value is always one config toggle away.
//
// Plain substring tests rather than one regexp: this runs once per variable
// every time the tab is (re)rendered, and a compiled alternation measured ~4x
// slower on a 300-variable environment for exactly the same answers.
var secretNameFragments = []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH"}

// secretNameSuffix catches the ..._KEY family (ANTHROPIC_API_KEY, SSH_KEY) as a
// suffix rather than a fragment, so KEYBOARD_LAYOUT and friends stay visible.
const secretNameSuffix = "_KEY"

// looksLikeSecret reports whether a variable's name suggests its value should
// not be on screen.
func looksLikeSecret(name string) bool {
	upper := strings.ToUpper(name)

	if strings.HasSuffix(upper, secretNameSuffix) {
		return true
	}

	for _, fragment := range secretNameFragments {
		if strings.Contains(upper, fragment) {
			return true
		}
	}

	return false
}

// maskedValue is what replaces a masked variable's value. Fixed-width on
// purpose: a mask whose length tracked the secret would leak its length.
const maskedValue = "••••••"

// envTabContent renders the env tab: a count line, then every variable the
// session's shell was launched with, sorted by name.
//
// Sorted rather than left in os.Environ order, which is the inherited
// environment's own arbitrary order followed by whatever buildEnv appended —
// stable output is what makes the panel readable and its test writable.
func (gui *Gui) envTabContent(env []string) string {
	if len(env) == 0 {
		return "  " + gui.tr.T("env.empty")
	}

	mask := gui.maskSecrets

	entries := make([]string, 0, len(env))
	masked := 0

	for _, entry := range env {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			// os.Environ can technically carry a nameless entry; showing it
			// verbatim beats dropping it silently.
			entries = append(entries, "  "+entry)

			continue
		}

		if mask && looksLikeSecret(name) {
			value = maskedValue
			masked++
		}

		entries = append(entries, "  "+name+"="+value)
	}

	sort.Strings(entries)

	header := "  " + gui.tr.T("env.count", len(env))
	if masked > 0 {
		header += gui.tr.T("env.masked", masked)
	}

	return header + "\n\n" + strings.Join(entries, "\n")
}
