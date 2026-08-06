package config

import (
	"os"
	"path/filepath"
	"testing"
)

// chdir moves the process into dir for the duration of the test: ProjectPath
// looks at the current directory by design, so there is no way to exercise its
// conventional-name rules without moving.
func chdir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}

	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func TestProjectPathPriority(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lazyshell.yml"), "sessions: []\n")
	writeFile(t, filepath.Join(dir, ".lazyshell.yml"), "sessions: []\n")
	chdir(t, dir)

	t.Setenv("LAZYSHELL_PROJECT_CONFIG", "")

	// Both conventional names present: the non-hidden one wins.
	if got := ProjectPath(""); got != "lazyshell.yml" {
		t.Errorf("ProjectPath(\"\") = %q, want lazyshell.yml", got)
	}

	// The environment beats the conventional names...
	t.Setenv("LAZYSHELL_PROJECT_CONFIG", "/from/env.yml")
	if got := ProjectPath(""); got != "/from/env.yml" {
		t.Errorf("ProjectPath with env = %q, want /from/env.yml", got)
	}

	// ...and the flag beats the environment.
	if got := ProjectPath("/from/flag.yml"); got != "/from/flag.yml" {
		t.Errorf("ProjectPath with flag = %q, want /from/flag.yml", got)
	}
}

func TestProjectPathFallsBackToHiddenName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".lazyshell.yml"), "sessions: []\n")
	chdir(t, dir)
	t.Setenv("LAZYSHELL_PROJECT_CONFIG", "")

	if got := ProjectPath(""); got != ".lazyshell.yml" {
		t.Errorf("ProjectPath = %q, want .lazyshell.yml", got)
	}
}

func TestProjectPathNoFile(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("LAZYSHELL_PROJECT_CONFIG", "")

	if got := ProjectPath(""); got != "" {
		t.Errorf("ProjectPath with no project file = %q, want empty", got)
	}
}

// A directory named lazyshell.yml is not a project file.
func TestProjectPathIgnoresDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lazyshell.yml"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	chdir(t, dir)
	t.Setenv("LAZYSHELL_PROJECT_CONFIG", "")

	if got := ProjectPath(""); got != "" {
		t.Errorf("ProjectPath = %q, want empty", got)
	}
}

func TestProjectPathExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("pas de répertoire home résolvable")
	}

	want := filepath.Join(home, "custom.yml")
	if got := ProjectPath("~/custom.yml"); got != want {
		t.Errorf("ProjectPath(\"~/custom.yml\") = %q, want %q", got, want)
	}
}

func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lazyshell.yml")
	writeFile(t, path, `shell: /bin/zsh
sessions:
  - name: api
    cwd: .
    command: make dev
    env:
      PORT: "3000"
  - name: shell
`)

	pcfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	if pcfg.Shell != "/bin/zsh" {
		t.Errorf("Shell = %q, want /bin/zsh", pcfg.Shell)
	}
	if len(pcfg.Sessions) != 2 {
		t.Fatalf("len(Sessions) = %d, want 2", len(pcfg.Sessions))
	}
	if pcfg.Sessions[0].Command != "make dev" {
		t.Errorf("Sessions[0].Command = %q, want make dev", pcfg.Sessions[0].Command)
	}
	if pcfg.Sessions[0].Env["PORT"] != "3000" {
		t.Errorf("Sessions[0].Env[PORT] = %q, want 3000", pcfg.Sessions[0].Env["PORT"])
	}
	if !filepath.IsAbs(pcfg.Path) {
		t.Errorf("Path = %q, want an absolute path", pcfg.Path)
	}
	if len(pcfg.Raw) == 0 {
		t.Error("Raw is empty, want the file's content (the trust hash needs it)")
	}
	if len(pcfg.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", pcfg.Warnings)
	}
}

func TestLoadProjectMissingFile(t *testing.T) {
	// Unlike the user config, an explicitly named project file that is not there
	// must be an error rather than a silent no-op.
	if _, err := LoadProject(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("LoadProject on a missing file: want error, got nil")
	}
}

// The whitelist decision made in phase 6: a project file can set shell and
// sessions, and nothing else. A repository must not be able to remap the user's
// keys by being cloned.
func TestProjectCannotOverrideThemeOrKeybindings(t *testing.T) {
	pcfg, err := ParseProject("/p/lazyshell.yml", []byte(`shell: /bin/zsh
prefix_key: Ctrl+X
keybindings:
  quit: Z
theme:
  active_border_color: red
sessions:
  - name: api
`))
	if err != nil {
		t.Fatalf("ParseProject: %v", err)
	}

	if len(pcfg.Warnings) != 3 {
		t.Errorf("Warnings = %v, want one per ignored key (prefix_key, keybindings, theme)", pcfg.Warnings)
	}

	user := Default()
	user.PrefixKey = "Ctrl+B"
	user.Keybindings = map[string]string{"quit": "q"}
	user.Theme.ActiveBorderColor = "green"

	merged := user.MergeProject(pcfg)

	if merged.Shell != "/bin/zsh" {
		t.Errorf("Shell = %q, want the project's /bin/zsh", merged.Shell)
	}
	if merged.PrefixKey != "Ctrl+B" {
		t.Errorf("PrefixKey = %q, want the user's Ctrl+B", merged.PrefixKey)
	}
	if merged.Keybindings["quit"] != "q" {
		t.Errorf("Keybindings[quit] = %q, want the user's q", merged.Keybindings["quit"])
	}
	if merged.Theme.ActiveBorderColor != "green" {
		t.Errorf("Theme.ActiveBorderColor = %q, want the user's green", merged.Theme.ActiveBorderColor)
	}
}

func TestMergeProjectKeepsUserShellWhenProjectIsSilent(t *testing.T) {
	user := Default()
	user.Shell = "/bin/fish"

	if got := user.MergeProject(ProjectConfig{}).Shell; got != "/bin/fish" {
		t.Errorf("Shell = %q, want the user's /bin/fish", got)
	}
}

func TestParseProjectMalformedYAML(t *testing.T) {
	if _, err := ParseProject("/p/lazyshell.yml", []byte("sessions: [oops\n")); err == nil {
		t.Fatal("ParseProject with malformed YAML: want error, got nil")
	}
}

// cwd is resolved against the config file's directory, not the process's — the
// whole point being that `cwd: ./services/api` means the same thing wherever
// lazyshell was invoked from.
func TestResolveCwdIsRelativeToTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Deliberately somewhere else, so a resolution against the process's cwd
	// would fail this test rather than pass it by accident.
	chdir(t, t.TempDir())

	got, err := SessionSpec{Cwd: "./services/api"}.ResolveCwd(dir)
	if err != nil {
		t.Fatalf("ResolveCwd: %v", err)
	}

	want, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}

	gotEval, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}

	if gotEval != want {
		t.Errorf("ResolveCwd = %q, want %q", gotEval, want)
	}
}

func TestResolveCwdEmptyMeansConfigDir(t *testing.T) {
	dir := t.TempDir()

	got, err := SessionSpec{}.ResolveCwd(dir)
	if err != nil {
		t.Fatalf("ResolveCwd: %v", err)
	}

	if got != dir {
		t.Errorf("ResolveCwd(\"\") = %q, want the config's directory %q", got, dir)
	}
}

func TestResolveCwdRejectsFilesAndMissingDirs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a-file")
	writeFile(t, file, "")

	if _, err := (SessionSpec{Cwd: "a-file"}).ResolveCwd(dir); err == nil {
		t.Error("ResolveCwd on a regular file: want error, got nil")
	}

	if _, err := (SessionSpec{Cwd: "nope"}).ResolveCwd(dir); err == nil {
		t.Error("ResolveCwd on a missing directory: want error, got nil")
	}
}

// One bad entry must never cost the user the other sessions.
func TestValidateDropsBadEntriesAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lazyshell.yml")
	writeFile(t, path, `sessions:
  - name: ok
  - name: ""
  - name: missing-dir
    cwd: ./nope
  - name: ok
  - name: second-ok
`)

	pcfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	valid, errs := pcfg.Validate()

	if len(valid) != 2 {
		t.Fatalf("valid = %v, want the 2 usable entries", valid)
	}
	if valid[0].Name != "ok" || valid[1].Name != "second-ok" {
		t.Errorf("valid = %v, want [ok second-ok] in file order", valid)
	}
	if len(errs) != 3 {
		t.Errorf("errs = %v, want one per bad entry (empty name, missing cwd, duplicate name)", errs)
	}
}

func TestValidateCarriesEnvAndCommand(t *testing.T) {
	pcfg := ProjectConfig{
		Path: filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{{
			Name:    "api",
			Command: "make dev",
			Env:     map[string]string{"PORT": "3000"},
		}},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if len(valid) != 1 {
		t.Fatalf("len(valid) = %d, want 1", len(valid))
	}
	if valid[0].Command != "make dev" || valid[0].Env["PORT"] != "3000" {
		t.Errorf("valid[0] = %+v, want command and env carried through", valid[0])
	}
}
