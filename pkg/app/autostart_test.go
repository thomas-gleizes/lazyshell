package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// newTestApp runs the real bootstrap — real config loading, real pty-backed
// sessions — with only the approval prompt's two ends replaced, and kills
// everything it started before the test returns.
func newTestApp(t *testing.T, opts Options, approve approver) *App {
	t.Helper()

	var stderr bytes.Buffer

	a := newApp(opts, approve, &stderr)
	t.Cleanup(a.sessions.Shutdown)

	return a
}

// answering builds an approver that types answer at the prompt. A fresh reader
// per call, since a shared one would be drained by the first test to use it.
func answering(answer string) approver {
	return approver{in: strings.NewReader(answer + "\n"), out: io.Discard, interactive: true}
}

// nonInteractive is what a piped stdin (CI, a test) looks like: no prompt is
// possible, so approval is refused.
var nonInteractive = approver{in: strings.NewReader(""), out: io.Discard, interactive: false}

// projectDir builds a temp directory holding a lazyshell.yml plus the
// subdirectories it references, moves the process into it, and isolates the
// user config and trust store from the real home directory.
func projectDir(t *testing.T, content string, subdirs ...string) string {
	t.Helper()

	// Resolved, not t.TempDir()'s raw answer: on macOS TMPDIR sits under
	// /var, which is a symlink to /private/var, and the moment this helper
	// chdir's into it every path the app reports back comes from os.Getwd —
	// i.e. resolved. A test comparing its own unresolved string against that
	// fails on a difference that means nothing: both name the same directory.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "lazyshell.yml"), []byte(content), 0o600); err != nil {
			t.Fatalf("write lazyshell.yml: %v", err)
		}
	}

	t.Setenv("LAZYSHELL_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	t.Setenv("LAZYSHELL_TRUST", filepath.Join(t.TempDir(), "trust.yml"))
	t.Setenv("LAZYSHELL_PROJECT_CONFIG", "")
	t.Setenv("SHELL", "/bin/sh")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	return dir
}

const threeSessions = `sessions:
  - name: api
    cwd: ./services/api
    command: echo demarree-api
    env:
      LAZYSHELL_TEST_PORT: "3000"
  - name: web
    cwd: ./web
  - name: shell
`

// The phase's exit criterion: three declared sessions all start, in file order,
// each in its own directory.
func TestAutostartCreatesEverySessionInOrder(t *testing.T) {
	dir := projectDir(t, threeSessions, "services/api", "web")

	a := newTestApp(t, Options{}, answering("y"))

	sessions := a.sessions.List()
	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d, want 3", len(sessions))
	}

	for i, want := range []string{"api", "web", "shell"} {
		if got := sessions[i].Name(); got != want {
			t.Errorf("sessions[%d].Name() = %q, want %q (file order)", i, got, want)
		}
	}

	for i, want := range []string{"services/api", "web", ""} {
		want := filepath.Join(dir, want)
		if got := sessions[i].Cwd; got != want {
			t.Errorf("sessions[%d].Cwd = %q, want %q", i, got, want)
		}
	}
}

// A project's env_files and a session's own layer in, and a session's
// no_default_env skips its automatic "<cwd>/.env" — the end-to-end path
// through app.newApp/autostart, not just pkg/config.Validate in isolation.
func TestAutostartLoadsEnvFiles(t *testing.T) {
	dir := projectDir(t, `env_files:
  - shared.env
sessions:
  - name: api
    cwd: ./services/api
    env_files:
      - services/api/api.env
    command: echo port=$LAZYSHELL_TEST_PORT shared=$LAZYSHELL_TEST_SHARED
  - name: web
    cwd: ./web
    no_default_env: true
    command: echo web-port=[$LAZYSHELL_TEST_WEB_PORT]
`, "services/api", "web")

	writeFile(t, filepath.Join(dir, "shared.env"), "LAZYSHELL_TEST_SHARED=base\nLAZYSHELL_TEST_PORT=1111\n")
	writeFile(t, filepath.Join(dir, "services/api/api.env"), "LAZYSHELL_TEST_PORT=3000\n")
	writeFile(t, filepath.Join(dir, "web/.env"), "LAZYSHELL_TEST_WEB_PORT=9999\n")

	a := newTestApp(t, Options{}, answering("y"))

	sessions := a.sessions.List()
	waitForScreen(t, sessions[0], "port=3000 shared=base")
	waitForScreen(t, sessions[1], "web-port=[]")
}

// --env-file applies to every session this run starts, including the default
// single session when no project file declares any.
func TestCLIEnvFileAppliesToTheDefaultSession(t *testing.T) {
	dir := projectDir(t, "")
	envPath := filepath.Join(dir, "global.env")
	writeFile(t, envPath, "LAZYSHELL_TEST_GLOBAL=from-cli\n")

	a := newTestApp(t, Options{EnvFiles: []string{envPath}}, nonInteractive)

	sessions := a.sessions.List()
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1 (the default session)", len(sessions))
	}

	if _, err := sessions[0].Write([]byte("echo global=$LAZYSHELL_TEST_GLOBAL\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sessions[0], "global=from-cli")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAutostartRunsTheDeclaredCommandAndEnv(t *testing.T) {
	projectDir(t, threeSessions, "services/api", "web")

	a := newTestApp(t, Options{}, answering("y"))

	api := a.sessions.List()[0]
	waitForScreen(t, api, "demarree-api")

	if _, err := api.Write([]byte("echo port=$LAZYSHELL_TEST_PORT\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForScreen(t, api, "port=3000")
}

// Without a project file, the empty-list startup now gets a single default
// session instead of dropping the user into an empty panel.
func TestNoProjectFileStartsDefaultSession(t *testing.T) {
	projectDir(t, "")

	a := newTestApp(t, Options{}, answering("y"))

	if got := len(a.sessions.List()); got != 1 {
		t.Errorf("len(sessions) = %d, want 1 (default session)", got)
	}
	if got := a.gui.StartupError(); got != "" {
		t.Errorf("StartupError = %q, want empty", got)
	}
}

func TestNoAutostartOpensTheUIWithoutStartingAnything(t *testing.T) {
	projectDir(t, threeSessions, "services/api", "web")

	a := newTestApp(t, Options{NoAutostart: true}, answering("y"))

	if got := len(a.sessions.List()); got != 0 {
		t.Errorf("len(sessions) = %d, want 0", got)
	}
	if !strings.Contains(a.gui.StartupError(), "no-autostart") {
		t.Errorf("StartupError = %q, want it to explain why nothing started", a.gui.StartupError())
	}
}

// The trust model: an unapproved file runs nothing, and says so.
func TestUnapprovedProjectStartsNothing(t *testing.T) {
	projectDir(t, threeSessions, "services/api", "web")

	a := newTestApp(t, Options{}, nonInteractive)

	if got := len(a.sessions.List()); got != 0 {
		t.Errorf("len(sessions) = %d, want 0", got)
	}
	if !strings.Contains(a.gui.StartupError(), "non approuvé") {
		t.Errorf("StartupError = %q, want it to mention the missing approval", a.gui.StartupError())
	}
}

// Answering yes at the prompt is remembered: a second launch of the same file
// starts without asking, even with no way to answer.
func TestApprovalIsRemembered(t *testing.T) {
	projectDir(t, threeSessions, "services/api", "web")

	first := newTestApp(t, Options{}, answering("y"))
	if got := len(first.sessions.List()); got != 3 {
		t.Fatalf("first launch: len(sessions) = %d, want 3", got)
	}

	second := newTestApp(t, Options{}, nonInteractive)
	if got := len(second.sessions.List()); got != 3 {
		t.Errorf("second launch: len(sessions) = %d, want 3 (approval not remembered)", got)
	}
}

// ...and editing the file asks again.
func TestEditingTheProjectFileRevokesApproval(t *testing.T) {
	dir := projectDir(t, threeSessions, "services/api", "web")

	first := newTestApp(t, Options{}, answering("y"))
	if got := len(first.sessions.List()); got != 3 {
		t.Fatalf("first launch: len(sessions) = %d, want 3", got)
	}

	edited := threeSessions + "  - name: sournoise\n    command: echo pwned\n"
	if err := os.WriteFile(filepath.Join(dir, "lazyshell.yml"), []byte(edited), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	second := newTestApp(t, Options{}, nonInteractive)
	if got := len(second.sessions.List()); got != 0 {
		t.Errorf("after an edit: len(sessions) = %d, want 0 (approval must be asked again)", got)
	}
}

func TestRefusingAtThePromptStartsNothing(t *testing.T) {
	projectDir(t, threeSessions, "services/api", "web")

	a := newTestApp(t, Options{}, answering("n"))

	if got := len(a.sessions.List()); got != 0 {
		t.Errorf("len(sessions) = %d, want 0", got)
	}
}

// A bad entry costs only itself.
func TestInvalidEntryDoesNotStopTheOthers(t *testing.T) {
	projectDir(t, `sessions:
  - name: ok
  - name: cassee
    cwd: ./nexiste-pas
  - name: aussi-ok
`)

	a := newTestApp(t, Options{}, answering("y"))

	sessions := a.sessions.List()
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want the 2 valid entries", len(sessions))
	}
	if sessions[0].Name() != "ok" || sessions[1].Name() != "aussi-ok" {
		t.Errorf("sessions = %q/%q, want ok/aussi-ok", sessions[0].Name(), sessions[1].Name())
	}
	if !strings.Contains(a.gui.StartupError(), "cassee") {
		t.Errorf("StartupError = %q, want the broken entry named", a.gui.StartupError())
	}
}

// --config-file wins over the file sitting in the current directory.
func TestConfigFileFlagWins(t *testing.T) {
	projectDir(t, threeSessions, "services/api", "web")

	elsewhere := t.TempDir()
	other := filepath.Join(elsewhere, "autre.yml")
	if err := os.WriteFile(other, []byte("sessions:\n  - name: depuis-le-flag\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	a := newTestApp(t, Options{ConfigFile: other}, answering("y"))

	sessions := a.sessions.List()
	if len(sessions) != 1 || sessions[0].Name() != "depuis-le-flag" {
		t.Fatalf("sessions = %v, want the single session from the flagged file", sessions)
	}
	if sessions[0].Cwd != elsewhere {
		t.Errorf("Cwd = %q, want the flagged file's own directory %q", sessions[0].Cwd, elsewhere)
	}
}

// A project file can pick the shell; it cannot touch the keyboard.
func TestProjectShellIsUsedForAutostartedSessions(t *testing.T) {
	projectDir(t, "shell: /bin/sh\nsessions:\n  - name: api\n")

	a := newTestApp(t, Options{}, answering("y"))

	sessions := a.sessions.List()
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if got := sessions[0].Cmd.Path; got != "/bin/sh" {
		t.Errorf("shell = %q, want /bin/sh from the project file", got)
	}
}

// An unreadable project file is reported, not fatal.
func TestUnreadableProjectFileIsReported(t *testing.T) {
	projectDir(t, "")

	a := newTestApp(t, Options{ConfigFile: filepath.Join(t.TempDir(), "absent.yml")}, answering("y"))

	if got := len(a.sessions.List()); got != 1 {
		t.Errorf("len(sessions) = %d, want 1 (default session)", got)
	}
	if a.gui.StartupError() == "" {
		t.Error("StartupError is empty, want the read failure reported")
	}
}

// waitForScreen polls a session's rendered screen, the same way
// pkg/session's tests do: shell timing is not otherwise deterministic.
func waitForScreen(t *testing.T, sess *session.Session, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sess.Screen().Render(), want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q on screen:\n%s", want, sess.Screen().Render())
}

const groupedSessions = `groups:
  - name: services
  - name: agents

sessions:
  - name: api
    group: services
  - name: claude
    group: agents
  - name: scratch
`

// The end-to-end group path: a declared group reaches the running session's
// Group(), and the file's declaration order reaches the GUI as the order its
// headers are drawn in.
func TestAutostartCarriesGroupsToSessionsAndGui(t *testing.T) {
	projectDir(t, groupedSessions)

	a := newTestApp(t, Options{}, answering("y"))

	want := map[string]string{"api": "services", "claude": "agents", "scratch": ""}

	for _, sess := range a.sessions.List() {
		if got := sess.Group(); got != want[sess.Name()] {
			t.Errorf("session %q Group() = %q, want %q", sess.Name(), got, want[sess.Name()])
		}
	}

	if got := a.gui.GroupOrder(); len(got) != 2 || got[0] != "services" || got[1] != "agents" {
		t.Errorf("gui group order = %v, want [services agents] in declaration order", got)
	}
}
