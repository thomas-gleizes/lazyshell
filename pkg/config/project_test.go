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

// env_files resolve project-wide first, then a session's own get appended;
// no_default_env falls back from the session to the project when the session
// does not say anything.
func TestValidateResolvesEnvFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lazyshell.yml")

	noDefault := true
	pcfg := ProjectConfig{
		Path:         path,
		EnvFiles:     []string{".env.shared"},
		NoDefaultEnv: &noDefault,
		Sessions: []SessionSpec{
			{Name: "api", EnvFiles: []string{".env.api"}},
			{Name: "worker"},
		},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if len(valid) != 2 {
		t.Fatalf("len(valid) = %d, want 2", len(valid))
	}

	api := valid[0]
	wantFiles := []string{filepath.Join(dir, ".env.shared"), filepath.Join(dir, ".env.api")}
	if len(api.EnvFiles) != 2 || api.EnvFiles[0] != wantFiles[0] || api.EnvFiles[1] != wantFiles[1] {
		t.Errorf("api.EnvFiles = %v, want %v", api.EnvFiles, wantFiles)
	}
	if api.NoDefaultEnv == nil || *api.NoDefaultEnv != true {
		t.Errorf("api.NoDefaultEnv = %v, want the project's true (session sets nothing)", api.NoDefaultEnv)
	}

	worker := valid[1]
	wantWorkerFiles := []string{filepath.Join(dir, ".env.shared")}
	if len(worker.EnvFiles) != 1 || worker.EnvFiles[0] != wantWorkerFiles[0] {
		t.Errorf("worker.EnvFiles = %v, want %v", worker.EnvFiles, wantWorkerFiles)
	}
}

// A session's own no_default_env overrides the project's.
func TestValidateSessionNoDefaultEnvOverridesProject(t *testing.T) {
	sessionSetting := false
	pcfg := ProjectConfig{
		Path: filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{
			{Name: "api", NoDefaultEnv: &sessionSetting},
		},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if valid[0].NoDefaultEnv == nil || *valid[0].NoDefaultEnv != false {
		t.Errorf("NoDefaultEnv = %v, want the session's explicit false", valid[0].NoDefaultEnv)
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

func TestValidateCarriesWatch(t *testing.T) {
	pcfg := ProjectConfig{
		Path: filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{{
			Name:  "api",
			Watch: []WatchSpec{{Pattern: "ERR!", Notify: true}},
		}},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if len(valid) != 1 || len(valid[0].Watch) != 1 {
		t.Fatalf("valid = %+v, want one session carrying one watch entry", valid)
	}
	if valid[0].Watch[0] != (WatchSpec{Pattern: "ERR!", Notify: true}) {
		t.Errorf("valid[0].Watch[0] = %+v, want {ERR! true}", valid[0].Watch[0])
	}
}

// A bad regexp costs only that one watcher — the session it belongs to, and
// any other watcher declared alongside it, still start. Same "one bad entry
// must never cost the rest" doctrine as a bad groups: entry.
func TestValidateDropsInvalidWatchPattern(t *testing.T) {
	pcfg := ProjectConfig{
		Path: filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{{
			Name: "api",
			Watch: []WatchSpec{
				{Pattern: "[", Notify: true},
				{Pattern: "ERR!", Notify: true},
			},
		}},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one (the invalid pattern)", errs)
	}
	if len(valid) != 1 {
		t.Fatalf("len(valid) = %d, want 1: a bad watch entry must not cost the session", len(valid))
	}
	if len(valid[0].Watch) != 1 || valid[0].Watch[0].Pattern != "ERR!" {
		t.Errorf("valid[0].Watch = %+v, want only the valid entry to survive", valid[0].Watch)
	}
}

func TestValidateCarriesRestartPolicy(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want RestartPolicy
	}{
		{"", RestartNever},
		{"never", RestartNever},
		{"on-failure", RestartOnFailure},
		{"always", RestartAlways},
	} {
		pcfg := ProjectConfig{
			Path:     filepath.Join(t.TempDir(), "lazyshell.yml"),
			Sessions: []SessionSpec{{Name: "api", Restart: tc.raw}},
		}

		valid, errs := pcfg.Validate()
		if len(errs) != 0 {
			t.Fatalf("restart %q: errs = %v, want none", tc.raw, errs)
		}
		if len(valid) != 1 || valid[0].Restart != tc.want {
			t.Errorf("restart %q: got %+v, want Restart = %q", tc.raw, valid, tc.want)
		}
	}
}

// An unrecognized restart: value falls back to RestartNever with a warning —
// it does not cost the session, unlike a bad watch pattern: a single scalar
// with a safe default, not a list of independent entries.
func TestValidateDropsInvalidRestartFallsBackToNever(t *testing.T) {
	pcfg := ProjectConfig{
		Path:     filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{{Name: "api", Restart: "sometimes"}},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one (the invalid restart policy)", errs)
	}
	if len(valid) != 1 || valid[0].Restart != RestartNever {
		t.Fatalf("valid = %+v, want one session with Restart = never, not dropped", valid)
	}
}

// stop_on_failure: is a plain bool, carried through unresolved — unlike
// locked:, there is no heuristic default to fall back to, so declaring it
// true without a command is accepted but reported, never dropped.
func TestValidateCarriesStopOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spec     SessionSpec
		wantVal  bool
		wantErrs int
	}{
		{"default false", SessionSpec{Name: "api", Command: "make dev"}, false, 0},
		{"declared true with a command", SessionSpec{Name: "api", Command: "make dev", StopOnFailure: true}, true, 0},
		{"declared true without a command warns but is not dropped", SessionSpec{Name: "api", StopOnFailure: true}, true, 1},
	} {
		pcfg := ProjectConfig{
			Path:     filepath.Join(t.TempDir(), "lazyshell.yml"),
			Sessions: []SessionSpec{tc.spec},
		}

		valid, errs := pcfg.Validate()
		if len(errs) != tc.wantErrs {
			t.Fatalf("%s: errs = %v, want %d", tc.name, errs, tc.wantErrs)
		}
		if len(valid) != 1 || valid[0].StopOnFailure != tc.wantVal {
			t.Errorf("%s: got %+v, want StopOnFailure = %v", tc.name, valid, tc.wantVal)
		}
	}
}

// locked: resolves to a plain bool: what the file says if it says anything,
// else "this session declares a command" — the ADR 0012 rule, which is what
// keeps a stray keystroke from killing a declared `npm run dev`.
func TestValidateResolvesLocked(t *testing.T) {
	yes, no := true, false

	for _, tc := range []struct {
		name    string
		spec    SessionSpec
		want    bool
		comment string
	}{
		{"declared true", SessionSpec{Name: "api", Locked: &yes}, true, "declared wins"},
		{"declared false with a command", SessionSpec{Name: "api", Command: "npm run dev", Locked: &no}, false,
			"an explicit false is the whole point of being able to write the key"},
		{"undeclared with a command", SessionSpec{Name: "api", Command: "npm run dev"}, true, "the heuristic"},
		{"undeclared, whitespace-only command", SessionSpec{Name: "api", Command: "   "}, false, "not a command"},
		{"undeclared bare shell", SessionSpec{Name: "shell"}, false, "nothing to protect"},
	} {
		pcfg := ProjectConfig{
			Path:     filepath.Join(t.TempDir(), "lazyshell.yml"),
			Sessions: []SessionSpec{tc.spec},
		}

		valid, errs := pcfg.Validate()
		if len(errs) != 0 {
			t.Fatalf("%s: errs = %v, want none", tc.name, errs)
		}
		if len(valid) != 1 || valid[0].Locked != tc.want {
			t.Errorf("%s: Locked = %+v, want %v (%s)", tc.name, valid, tc.want, tc.comment)
		}
	}
}

// locked: is a per-session key. At the top level of the file it stays what it
// was before this feature: an unknown key, ignored with a warning.
func TestProjectLevelLockedIsAnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lazyshell.yml")
	writeFile(t, path, "locked: true\nsessions:\n  - name: api\n")

	pcfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	if len(pcfg.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one about the top-level locked:", pcfg.Warnings)
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 || len(valid) != 1 || valid[0].Locked {
		t.Errorf("valid = %+v, errs = %v, want the session kept and unlocked", valid, errs)
	}
}

// A project file declares its groups and assigns sessions to them. The group
// is trimmed and carried onto the ResolvedSession, and `groups:` fixes the
// display order.
func TestProjectGroupsParseAndResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lazyshell.yml")
	writeFile(t, path, `groups:
  - name: services
  - name: agents

sessions:
  - name: api
    group: "  services  "
  - name: claude
    group: agents
  - name: scratch
`)

	pcfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	if len(pcfg.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none: groups and group are whitelisted keys", pcfg.Warnings)
	}

	groups, errs := pcfg.ResolvedGroups()
	if len(errs) != 0 {
		t.Fatalf("ResolvedGroups errs = %v, want none", errs)
	}
	if len(groups) != 2 || groups[0] != "services" || groups[1] != "agents" {
		t.Errorf("ResolvedGroups() = %v, want [services agents] in declaration order", groups)
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Fatalf("Validate errs = %v, want none", errs)
	}

	want := []string{"services", "agents", ""}
	for i, group := range want {
		if valid[i].Group != group {
			t.Errorf("valid[%d].Group = %q, want %q", i, valid[i].Group, group)
		}
	}
}

// Naming a group no `groups:` block declares is legitimate, not an error: the
// block only fixes the header order, and `group: api` must work on its own.
func TestValidateAcceptsAnUndeclaredGroup(t *testing.T) {
	pcfg := ProjectConfig{
		Path:     filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{{Name: "api", Group: "never-declared"}},
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none: an undeclared group is not an error", errs)
	}
	if valid[0].Group != "never-declared" {
		t.Errorf("valid[0].Group = %q, want the undeclared group carried through", valid[0].Group)
	}
}

// Bad group declarations are dropped and reported, like bad sessions — one
// hand-edit slip must not cost the groups that were fine.
func TestResolvedGroupsDropsBadEntriesAndKeepsTheRest(t *testing.T) {
	pcfg := ProjectConfig{
		Path: filepath.Join(t.TempDir(), "lazyshell.yml"),
		Groups: []GroupSpec{
			{Name: "services"},
			{Name: "   "},
			{Name: "services"},
			{Name: "agents"},
		},
	}

	groups, errs := pcfg.ResolvedGroups()

	if len(groups) != 2 || groups[0] != "services" || groups[1] != "agents" {
		t.Errorf("ResolvedGroups() = %v, want [services agents]", groups)
	}
	if len(errs) != 2 {
		t.Errorf("errs = %v, want one per bad entry (empty name, duplicate)", errs)
	}
}

// A group name spanning two lines would tear the panel's single-line header
// in two, so it is the one shape that is rejected outright.
func TestValidateRejectsAMultilineGroupName(t *testing.T) {
	pcfg := ProjectConfig{
		Path:     filepath.Join(t.TempDir(), "lazyshell.yml"),
		Sessions: []SessionSpec{{Name: "api", Group: "two\nlines"}},
	}

	valid, errs := pcfg.Validate()
	if len(valid) != 0 {
		t.Errorf("valid = %v, want the entry dropped", valid)
	}
	if len(errs) != 1 {
		t.Errorf("errs = %v, want exactly one", errs)
	}
}
