package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
)

// The template is the first thing a new user sees, and it is written by us —
// shipping one our own loader warns about, or one whose values quietly differ
// from the defaults, would be worse than shipping nothing.
func TestUserConfigTemplateLoadsCleanly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(userConfigTemplate), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(template) = %v, want no error", err)
	}

	if len(cfg.Warnings) > 0 {
		t.Errorf("the template produced warnings %v — every key in it must be a real one", cfg.Warnings)
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Errorf("the template failed config.Validate: %v", errs)
	}

	if errs := gui.ValidateConfig(cfg); len(errs) > 0 {
		t.Errorf("the template failed gui.ValidateConfig: %v", errs)
	}

	// Same comparison as pkg/config's README test: keybindings and theme are
	// empty in Default() because their real defaults live in pkg/gui, and
	// gui.ValidateConfig above has already vouched for the values used here.
	cfg.Warnings, cfg.Keybindings, cfg.Theme = nil, nil, config.Theme{}

	want := config.Default()
	want.Keybindings, want.Theme = nil, config.Theme{}

	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("the template loads to\n%+v\nwant the defaults\n%+v", cfg, want)
	}
}

func TestInitUserConfigWritesAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yml")

	var out bytes.Buffer
	if err := InitUserConfig(path, &out); err != nil {
		t.Fatalf("InitUserConfig() = %v, want it to create the file and its parent directory", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the written config: %v", err)
	}

	if string(written) != userConfigTemplate {
		t.Error("the written file does not match the template")
	}

	if !strings.Contains(out.String(), path) {
		t.Errorf("output = %q, want it to name the file it created", out.String())
	}

	// The whole reason this uses O_EXCL: the file is hand-edited, and a second
	// `config init` must never be able to destroy that work.
	if err := os.WriteFile(path, []byte("language: en\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := InitUserConfig(path, &out); err == nil {
		t.Error("InitUserConfig() overwrote an existing config, it must refuse")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the config: %v", err)
	}

	if string(after) != "language: en\n" {
		t.Error("the existing config was modified by the refused init")
	}
}

func TestShowConfigPrintsEffectiveValuesAndSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	if err := os.WriteFile(path, []byte("sessions_panel_width: 50\nmarkers:\n  bell: \"*\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("LAZYSHELL_CONFIG", path)

	var out, errOut bytes.Buffer
	if err := ShowConfig(Options{}, &out, &errOut); err != nil {
		t.Fatalf("ShowConfig() = %v, want no error", err)
	}

	got := out.String()

	for _, want := range []string{
		path,                       // the source it read
		"sessions_panel_width: 50", // what the file overrode
		"bell: '*'",                // a nested override
		"scrollback_size: 10000",   // an untouched default, still reported
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ShowConfig() output missing %q:\n%s", want, got)
		}
	}
}

// The point of `config show` is that it reports what is *applied*, not what is
// written. A value the validator had to reject must come back corrected.
func TestShowConfigReportsCorrectedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")

	if err := os.WriteFile(path, []byte("refresh_interval_ms: 0\nlanguage: de\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("LAZYSHELL_CONFIG", path)

	var out, errOut bytes.Buffer
	if err := ShowConfig(Options{}, &out, &errOut); err != nil {
		t.Fatalf("ShowConfig() = %v, want no error", err)
	}

	if !strings.Contains(out.String(), "refresh_interval_ms: 30") {
		t.Errorf("output should show the corrected interval, got:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "language: fr") {
		t.Errorf("output should show the corrected language, got:\n%s", out.String())
	}

	for _, want := range []string{"refresh_interval_ms", "language"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr should explain the correction of %q, got:\n%s", want, errOut.String())
		}
	}
}

func TestParseArgsConfigSubcommand(t *testing.T) {
	tests := []struct {
		args    []string
		wantArg string
		wantErr bool
	}{
		{args: []string{"config"}, wantArg: ConfigShow},
		{args: []string{"config", "show"}, wantArg: ConfigShow},
		{args: []string{"config", "init"}, wantArg: ConfigInit},
		{args: []string{"config", "sho"}, wantErr: true},
	}

	for _, tt := range tests {
		inv, err := ParseArgs(tt.args)

		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseArgs(%v) = nil error, want a complaint about the unknown verb", tt.args)
			}

			continue
		}

		if err != nil {
			t.Errorf("ParseArgs(%v) = %v, want no error", tt.args, err)

			continue
		}

		if inv.Command != CommandConfig || inv.Arg != tt.wantArg {
			t.Errorf("ParseArgs(%v) = {%q %q}, want {%q %q}", tt.args, inv.Command, inv.Arg, CommandConfig, tt.wantArg)
		}
	}
}
