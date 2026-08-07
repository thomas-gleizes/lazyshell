package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestsBuiltins(t *testing.T) {
	d, warnings := LoadManifests("")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings loading built-ins: %v", warnings)
	}

	for _, process := range []string{"claude", "codex", "opencode"} {
		if _, ok := d.manifests[process]; !ok {
			t.Errorf("missing built-in manifest for %q", process)
		}
	}
}

func TestLoadManifestsOverrideReplacesBuiltin(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "claude.yml"), []byte(`
process: claude
rules:
  - state: done
    title_pattern: '.*'
`), 0o644); err != nil {
		t.Fatal(err)
	}

	d, warnings := LoadManifests(dir)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	if got := d.Evaluate("claude", "anything", "anything"); got != StateDone {
		t.Fatalf("got %v, want StateDone from the override manifest", got)
	}
}

func TestLoadManifestsOverrideAddsNewAgent(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "myagent.yml"), []byte(`
process: myagent
rules:
  - state: working
    screen_pattern: 'busy'
`), 0o644); err != nil {
		t.Fatal(err)
	}

	d, _ := LoadManifests(dir)

	if got := d.Evaluate("myagent", "busy now", ""); got != StateWorking {
		t.Fatalf("got %v, want StateWorking", got)
	}

	// Built-ins are still there alongside the addition.
	if _, ok := d.manifests["claude"]; !ok {
		t.Fatalf("override directory should add to, not replace, the built-in set")
	}
}

func TestLoadManifestsInvalidOverrideIsSkippedWithWarning(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "broken.yml"), []byte(`
process: broken
rules:
  - state: not-a-real-state
    screen_pattern: 'x'
`), 0o644); err != nil {
		t.Fatal(err)
	}

	d, warnings := LoadManifests(dir)
	if len(warnings) == 0 {
		t.Fatal("expected a warning for the invalid manifest")
	}

	if _, ok := d.manifests["broken"]; ok {
		t.Fatal("invalid manifest must not be registered")
	}

	// Built-ins must still load despite the broken override file.
	if _, ok := d.manifests["claude"]; !ok {
		t.Fatal("built-ins should still load when an override file is broken")
	}
}

func TestLoadManifestsMissingOverrideDirIsNotAnError(t *testing.T) {
	_, warnings := LoadManifests(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings for a missing override dir: %v", warnings)
	}
}

func TestBuiltinManifestsRespectBlockedFirst(t *testing.T) {
	entries, err := builtinManifests.ReadDir("manifests")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		data, err := builtinManifests.ReadFile("manifests/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}

		if _, err := parseManifest(data); err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
		}
	}
}
