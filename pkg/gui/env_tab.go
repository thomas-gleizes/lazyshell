package gui

import (
	"regexp"
	"sort"
	"strings"
)

// secretNamePattern is the heuristic behind env_tab.mask_secrets: the panel is
// as shareable as a screenshot of it, and an API key sitting in a session's
// environment must not be the thing that makes a screenshot dangerous.
//
// It matches on the variable's *name*, never its value — a value-based guess
// ("looks like a token") would both miss short secrets and hide innocuous
// paths. Deliberately broad: a masked variable is a small annoyance, a leaked
// one is not, and the value is always one config toggle away.
var secretNamePattern = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|AUTH|_KEY$|^.*_API_KEY)`)

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

		if mask && secretNamePattern.MatchString(name) {
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
