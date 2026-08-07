package agent

import "testing"

func mustManifest(t *testing.T, yaml string) Manifest {
	t.Helper()

	m, err := parseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("parseManifest: %v", err)
	}

	return m
}

func TestEvaluateNoManifestIsNone(t *testing.T) {
	d := NewDetector(map[string]Manifest{})

	if got := d.Evaluate("bash", "anything", ""); got != StateNone {
		t.Fatalf("got %v, want StateNone", got)
	}
}

func TestEvaluateUnknownProcessIsNone(t *testing.T) {
	m := mustManifest(t, `
process: claude
rules:
  - state: blocked
    screen_pattern: 'proceed'
`)
	d := NewDetector(map[string]Manifest{"claude": m})

	if got := d.Evaluate("vim", "proceed?", ""); got != StateNone {
		t.Fatalf("got %v, want StateNone", got)
	}
}

func TestEvaluateNoRuleMatchesIsIdle(t *testing.T) {
	m := mustManifest(t, `
process: claude
rules:
  - state: blocked
    screen_pattern: 'proceed'
`)
	d := NewDetector(map[string]Manifest{"claude": m})

	if got := d.Evaluate("claude", "just a normal prompt", ""); got != StateIdle {
		t.Fatalf("got %v, want StateIdle", got)
	}
}

func TestEvaluateFirstMatchWins(t *testing.T) {
	m := mustManifest(t, `
process: claude
rules:
  - state: blocked
    screen_pattern: 'do you want to proceed'
  - state: working
    screen_pattern: 'esc to interrupt'
`)
	d := NewDetector(map[string]Manifest{"claude": m})

	if got := d.Evaluate("claude", "esc to interrupt\ndo you want to proceed?", ""); got != StateBlocked {
		t.Fatalf("got %v, want StateBlocked (blocked rule listed first)", got)
	}
}

func TestEvaluateTitlePattern(t *testing.T) {
	m := mustManifest(t, `
process: codex
rules:
  - state: done
    title_pattern: '^codex$'
`)
	d := NewDetector(map[string]Manifest{"codex": m})

	if got := d.Evaluate("codex", "irrelevant screen", "codex"); got != StateDone {
		t.Fatalf("got %v, want StateDone", got)
	}

	if got := d.Evaluate("codex", "irrelevant screen", "codex - my-project"); got != StateIdle {
		t.Fatalf("got %v, want StateIdle (title anchored, should not match)", got)
	}
}

func TestEvaluateProcessMatchIsCaseInsensitive(t *testing.T) {
	m := mustManifest(t, `
process: Claude
rules:
  - state: working
    screen_pattern: 'thinking'
`)
	d := NewDetector(map[string]Manifest{"claude": m})

	if got := d.Evaluate("CLAUDE", "thinking...", ""); got != StateWorking {
		t.Fatalf("got %v, want StateWorking", got)
	}
}
