package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatePathIsStableAndDistinctPerCwd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	a1, err := StatePath("/some/project")
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}

	a2, err := StatePath("/some/project")
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}

	if a1 != a2 {
		t.Errorf("StatePath is not stable for the same cwd: %q vs %q", a1, a2)
	}

	b, err := StatePath("/some/other-project")
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}

	if a1 == b {
		t.Errorf("StatePath collided for two different directories: %q", a1)
	}
}

func TestSaveLoadStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cwd := filepath.Join(t.TempDir(), "project")
	sessions := []StateSession{
		{Name: "api", Group: "backend", Cwd: cwd, Command: "npm run dev"},
		{Name: "logs", Cwd: cwd},
	}

	if err := SaveState(cwd, sessions); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	state, err := LoadState(cwd)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if state == nil {
		t.Fatal("LoadState after SaveState = nil, want the saved state")
	}

	if len(state.Sessions) != len(sessions) {
		t.Fatalf("LoadState.Sessions = %v, want %v", state.Sessions, sessions)
	}

	for i, got := range state.Sessions {
		if got != sessions[i] {
			t.Errorf("session #%d = %+v, want %+v", i, got, sessions[i])
		}
	}
}

func TestLoadStateMissingIsNilNotError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	state, err := LoadState(filepath.Join(t.TempDir(), "never-saved"))
	if err != nil {
		t.Fatalf("LoadState(missing) = %v, want no error", err)
	}

	if state != nil {
		t.Errorf("LoadState(missing) = %+v, want nil", state)
	}
}

// A state file is never trust-gated (see docs/adr/0013), but its permissions
// are still the one thing standing between it and any other process on the
// machine — SaveState writes 0600, and LoadState must refuse anything looser
// rather than silently trusting it.
func TestLoadStateIgnoresLooserPermissions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cwd := filepath.Join(t.TempDir(), "project")
	if err := SaveState(cwd, []StateSession{{Name: "api", Cwd: cwd}}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	path, err := StatePath(cwd)
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	state, err := LoadState(cwd)
	if err != nil {
		t.Fatalf("LoadState(loose permissions) = %v, want no error", err)
	}

	if state != nil {
		t.Errorf("LoadState(loose permissions) = %+v, want nil (ignored, not trusted)", state)
	}
}
