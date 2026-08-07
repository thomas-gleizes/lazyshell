package agent

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// rawManifest is the YAML shape a manifest file is written in: patterns as
// plain strings, compiled into Manifest by parseManifest.
type rawManifest struct {
	Process string    `yaml:"process"`
	Rules   []rawRule `yaml:"rules"`
}

type rawRule struct {
	State         string `yaml:"state"`
	ScreenPattern string `yaml:"screen_pattern"`
	TitlePattern  string `yaml:"title_pattern"`
}

// Manifest declares how to recognize one agent CLI (by its foreground
// process name) and how to read its state off the screen it draws — no Go
// code, so a UI change in the agent is a manifest edit, not a lazyshell
// release.
type Manifest struct {
	// Process is the foreground process name (as reported by the OS, e.g.
	// Linux's /proc/<pid>/comm) that selects this manifest. Matched
	// case-insensitively.
	Process string
	Rules   []Rule
}

// Rule is one line of a manifest: if its pattern(s) match, the session is in
// State. Rules are evaluated in file order and the first match wins, which is
// what lets a manifest author put its "blocked" rules first — see
// ValidateBlockedFirst.
type Rule struct {
	State State
	// ScreenPattern, if set, must match the plain-text tail of the visible
	// screen. TitlePattern, if set, must match the terminal title (OSC 0/2).
	// At least one of the two is set; when both are, the rule only matches
	// when both do.
	ScreenPattern *regexp.Regexp
	TitlePattern  *regexp.Regexp
}

// parseManifest compiles a manifest's YAML source. It never returns a
// partially-valid Manifest: any bad regex or empty process name is an error,
// and the caller is expected to skip the file and warn rather than treat
// this as fatal — a broken manifest degrades detection for one agent, not
// the whole feature.
func parseManifest(data []byte) (Manifest, error) {
	var raw rawManifest
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Manifest{}, fmt.Errorf("agent: parse manifest: %w", err)
	}

	if raw.Process == "" {
		return Manifest{}, fmt.Errorf("agent: manifest has no process")
	}

	m := Manifest{Process: raw.Process}

	for i, rr := range raw.Rules {
		state, ok := parseState(rr.State)
		if !ok {
			return Manifest{}, fmt.Errorf("agent: %s: rule %d: unknown state %q", raw.Process, i, rr.State)
		}

		if rr.ScreenPattern == "" && rr.TitlePattern == "" {
			return Manifest{}, fmt.Errorf("agent: %s: rule %d: neither screen_pattern nor title_pattern set", raw.Process, i)
		}

		rule := Rule{State: state}

		if rr.ScreenPattern != "" {
			re, err := regexp.Compile(rr.ScreenPattern)
			if err != nil {
				return Manifest{}, fmt.Errorf("agent: %s: rule %d: screen_pattern: %w", raw.Process, i, err)
			}
			rule.ScreenPattern = re
		}

		if rr.TitlePattern != "" {
			re, err := regexp.Compile(rr.TitlePattern)
			if err != nil {
				return Manifest{}, fmt.Errorf("agent: %s: rule %d: title_pattern: %w", raw.Process, i, err)
			}
			rule.TitlePattern = re
		}

		m.Rules = append(m.Rules, rule)
	}

	if err := validateBlockedFirst(m); err != nil {
		return Manifest{}, err
	}

	return m, nil
}

// validateBlockedFirst enforces the one non-negotiable rule from the design
// report: "blocked" must stay strict, which here means every "blocked" rule
// precedes every non-"blocked" rule. A manifest that lists "working" before
// "blocked" would let a broader working-pattern shadow the precise
// permission-prompt pattern it is meant to defer to.
func validateBlockedFirst(m Manifest) error {
	seenOther := false

	for i, rule := range m.Rules {
		if rule.State == StateBlocked {
			if seenOther {
				return fmt.Errorf("agent: %s: rule %d: blocked rules must precede all others", m.Process, i)
			}

			continue
		}

		seenOther = true
	}

	return nil
}

// matches reports whether the rule fires against a session's current screen
// tail and title.
func (r Rule) matches(screenTail, title string) bool {
	if r.ScreenPattern != nil && !r.ScreenPattern.MatchString(screenTail) {
		return false
	}

	if r.TitlePattern != nil && !r.TitlePattern.MatchString(title) {
		return false
	}

	return true
}
