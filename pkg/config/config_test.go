package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.ScrollbackSize != defaultScrollbackSize {
		t.Errorf("ScrollbackSize = %d, want %d", cfg.ScrollbackSize, defaultScrollbackSize)
	}
	if cfg.SessionsPanelWidth != defaultSessionsPanelWidth {
		t.Errorf("SessionsPanelWidth = %d, want %d", cfg.SessionsPanelWidth, defaultSessionsPanelWidth)
	}
	if cfg.PrefixKey != defaultPrefixKey {
		t.Errorf("PrefixKey = %q, want %q", cfg.PrefixKey, defaultPrefixKey)
	}
	if cfg.Shell != "" {
		t.Errorf("Shell = %q, want empty (resolved at use)", cfg.Shell)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("Load on a missing file = %+v, want Default()", cfg)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("Load(\"\") = %+v, want Default()", cfg)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	writeFile(t, path, "shell: /bin/zsh\nscrollback_size: 500\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Shell != "/bin/zsh" {
		t.Errorf("Shell = %q, want /bin/zsh", cfg.Shell)
	}
	if cfg.ScrollbackSize != 500 {
		t.Errorf("ScrollbackSize = %d, want 500", cfg.ScrollbackSize)
	}
	// Fields absent from the file must keep their default value.
	if cfg.SessionsPanelWidth != defaultSessionsPanelWidth {
		t.Errorf("SessionsPanelWidth = %d, want default %d", cfg.SessionsPanelWidth, defaultSessionsPanelWidth)
	}
	if cfg.PrefixKey != defaultPrefixKey {
		t.Errorf("PrefixKey = %q, want default %q", cfg.PrefixKey, defaultPrefixKey)
	}
}

func TestLoadKeybindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	writeFile(t, path, "keybindings:\n  new_session: N\n  quit: Ctrl+Q\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := map[string]string{"new_session": "N", "quit": "Ctrl+Q"}
	if len(cfg.Keybindings) != len(want) {
		t.Fatalf("Keybindings = %v, want %v", cfg.Keybindings, want)
	}
	for action, key := range want {
		if cfg.Keybindings[action] != key {
			t.Errorf("Keybindings[%q] = %q, want %q", action, cfg.Keybindings[action], key)
		}
	}
}

func TestLoadThemeOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	writeFile(t, path, "theme:\n  active_border_color: yellow\n  selected_bg_color: cyan\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Theme.ActiveBorderColor != "yellow" {
		t.Errorf("Theme.ActiveBorderColor = %q, want yellow", cfg.Theme.ActiveBorderColor)
	}
	if cfg.Theme.SelectedBgColor != "cyan" {
		t.Errorf("Theme.SelectedBgColor = %q, want cyan", cfg.Theme.SelectedBgColor)
	}
	// Fields absent from the theme block must keep their (empty) default.
	if cfg.Theme.InactiveBorderColor != "" {
		t.Errorf("Theme.InactiveBorderColor = %q, want empty default", cfg.Theme.InactiveBorderColor)
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	writeFile(t, path, "shell: [this is not a string\n")

	if _, err := Load(path); err == nil {
		t.Fatal("Load with malformed YAML: want error, got nil")
	}
}

func TestPathEnvOverride(t *testing.T) {
	t.Setenv("LAZYSHELL_CONFIG", "/custom/path/config.yml")

	if got := Path(); got != "/custom/path/config.yml" {
		t.Errorf("Path() = %q, want /custom/path/config.yml", got)
	}
}

func TestPathXDGConfigHome(t *testing.T) {
	t.Setenv("LAZYSHELL_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	want := filepath.Join("/xdg", "lazyshell", "config.yml")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
