package agent

import "strings"

// Detector holds every loaded manifest, keyed by process name, and turns a
// session's foreground process plus its screen snapshot into a State. It
// does no I/O of its own — LoadManifests builds it once at startup, and
// pkg/session calls Evaluate from its drain goroutine, throttled.
type Detector struct {
	manifests map[string]Manifest
}

// NewDetector builds a Detector from an already-loaded set of manifests, one
// per process name. Exported mainly for tests; LoadManifests is what
// pkg/session actually calls.
func NewDetector(manifests map[string]Manifest) *Detector {
	return &Detector{manifests: manifests}
}

// Evaluate returns the agent state for a session whose foreground process is
// processName, given the plain-text tail of its visible screen and its
// terminal title. StateNone means processName matches no known manifest —
// this is not an agent session, and the caller must not render a marker for
// it. A manifest match with no rule firing is StateIdle.
func (d *Detector) Evaluate(processName, screenTail, title string) State {
	if d == nil || processName == "" {
		return StateNone
	}

	m, ok := d.manifests[strings.ToLower(processName)]
	if !ok {
		return StateNone
	}

	for _, rule := range m.Rules {
		if rule.matches(screenTail, title) {
			return rule.State
		}
	}

	return StateIdle
}

// Name reports the manifest's declared name for processName (e.g. "claude",
// "codex", "opencode") — false if no manifest matches. The same lookup
// Evaluate does internally, split out so a caller only interested in which
// agent this is (pkg/gui's dashboard) does not need a screen tail or title to
// ask for it.
func (d *Detector) Name(processName string) (string, bool) {
	if d == nil || processName == "" {
		return "", false
	}

	m, ok := d.manifests[strings.ToLower(processName)]
	if !ok {
		return "", false
	}

	return m.Process, true
}
