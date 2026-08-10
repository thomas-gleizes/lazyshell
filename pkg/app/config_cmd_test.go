package app

import (
	"bytes"
	"errors"
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

// The two things `config edit` must do beyond spawning an editor: create the
// commented template when there is nothing to edit yet, and hand the editor the
// path it just resolved.
func TestEditConfigCreatesThenEditsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yml")

	t.Setenv("LAZYSHELL_CONFIG", path)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "my-editor --wait")

	var got []string

	var out, errOut bytes.Buffer

	run := func(argv []string) error {
		got = argv

		return nil
	}

	if err := editConfig(path, &out, &errOut, failingLookPath(t), run); err != nil {
		t.Fatalf("editConfig() = %v, want no error", err)
	}

	if want := []string{"my-editor", "--wait", path}; !reflect.DeepEqual(got, want) {
		t.Errorf("editor argv = %v, want %v", got, want)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the created config: %v", err)
	}

	if string(written) != userConfigTemplate {
		t.Error("the file handed to the editor is not the commented template")
	}

	if !strings.Contains(out.String(), "aucun problème") {
		t.Errorf("output = %q, want it to report the saved file as clean", out.String())
	}
}

// An existing file is edited as it is: `config edit` must never be a way to
// lose a hand-written config, which is also why it goes through InitUserConfig
// (O_EXCL) rather than writing the template itself.
func TestEditConfigKeepsAnExistingFileAndReportsItsProblems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	existing := "sessions_panel_width: 50\nrefresh_interval_ms: 0\nnot_a_key: 1\n"

	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("LAZYSHELL_CONFIG", path)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "my-editor")

	var out, errOut bytes.Buffer

	if err := editConfig(path, &out, &errOut, failingLookPath(t), func([]string) error { return nil }); err != nil {
		t.Fatalf("editConfig() = %v, want no error", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the config: %v", err)
	}

	if string(after) != existing {
		t.Error("the existing config was rewritten, it must be edited as-is")
	}

	for _, want := range []string{"refresh_interval_ms", "not_a_key"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr should report %q after the edit, got:\n%s", want, errOut.String())
		}
	}

	if strings.Contains(out.String(), "aucun problème") {
		t.Errorf("output = %q, want no clean bill of health for a file with problems", out.String())
	}
}

// A file that no longer parses is the one outcome worth a non-zero exit: it is
// exactly what the next start would fall back to defaults over.
func TestEditConfigFailsOnUnparseableResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")

	if err := os.WriteFile(path, []byte("language: fr\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("LAZYSHELL_CONFIG", path)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "my-editor")

	corrupt := func([]string) error {
		return os.WriteFile(path, []byte("markers: [oops\n"), 0o600)
	}

	var out, errOut bytes.Buffer
	if err := editConfig(path, &out, &errOut, failingLookPath(t), corrupt); err == nil {
		t.Error("editConfig() = nil, want the parse error of the saved file")
	}
}

// The editor exiting non-zero must be reported, and must not be mistaken for a
// problem with the configuration.
func TestEditConfigReportsEditorFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")

	t.Setenv("LAZYSHELL_CONFIG", path)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "my-editor")

	var out, errOut bytes.Buffer

	err := editConfig(path, &out, &errOut, failingLookPath(t), func([]string) error {
		return errors.New("exit status 1")
	})
	if err == nil {
		t.Fatal("editConfig() = nil, want the editor's failure")
	}

	if !strings.Contains(err.Error(), "my-editor") {
		t.Errorf("error = %v, want it to name the editor it ran", err)
	}
}

func TestResolveEditor(t *testing.T) {
	// $VISUAL wins over $EDITOR: we have a real terminal here, and that is the
	// whole point of the distinction.
	t.Run("visual beats editor", func(t *testing.T) {
		t.Setenv("VISUAL", "full-screen")
		t.Setenv("EDITOR", "line-editor")

		argv, err := resolveEditor(failingLookPath(t))
		if err != nil {
			t.Fatalf("resolveEditor() = %v", err)
		}

		if !reflect.DeepEqual(argv, []string{"full-screen"}) {
			t.Errorf("resolveEditor() = %v, want [full-screen]", argv)
		}
	})

	// An empty (or whitespace-only) variable is not a choice of editor.
	t.Run("falls back to the first installed editor", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "   ")

		lookPath := func(name string) (string, error) {
			if name == "vim" {
				return "/usr/bin/vim", nil
			}

			return "", errors.New("not found")
		}

		argv, err := resolveEditor(lookPath)
		if err != nil {
			t.Fatalf("resolveEditor() = %v", err)
		}

		if !reflect.DeepEqual(argv, []string{"vim"}) {
			t.Errorf("resolveEditor() = %v, want [vim], the first of %v that is installed", argv, fallbackEditors)
		}
	})

	t.Run("says what to do when there is none", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")

		_, err := resolveEditor(func(string) (string, error) { return "", errors.New("not found") })
		if err == nil {
			t.Fatal("resolveEditor() = nil error, want a complaint naming $EDITOR")
		}

		if !strings.Contains(err.Error(), "EDITOR") {
			t.Errorf("error = %v, want it to tell the user to set $EDITOR", err)
		}
	})
}

// failingLookPath is for the cases where the editor comes from the environment:
// consulting $PATH there would mean the test's outcome depends on what is
// installed on the machine running it.
func failingLookPath(t *testing.T) func(string) (string, error) {
	t.Helper()

	return func(name string) (string, error) {
		t.Errorf("resolveEditor looked up %q in $PATH, want the environment to have decided", name)

		return "", errors.New("not found")
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
		{args: []string{"config", "edit"}, wantArg: ConfigEdit},
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
