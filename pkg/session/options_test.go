package session

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWithOptionsPassesEnv(t *testing.T) {
	m := newTestManager(t)

	sess, err := m.NewWithOptions(Options{
		Name:  "api",
		Shell: testShell,
		Env:   map[string]string{"LAZYSHELL_TEST_PORT": "3000"},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo port=$LAZYSHELL_TEST_PORT\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "port=3000")
}

// An entry's env must not leak into the sessions declared next to it.
func TestNewWithOptionsEnvIsPerSession(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.NewWithOptions(Options{
		Name:  "with-env",
		Shell: testShell,
		Env:   map[string]string{"LAZYSHELL_TEST_LEAK": "yes"},
	}); err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	other, err := m.NewWithOptions(Options{Name: "without-env", Shell: testShell})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := other.Write([]byte("echo leak=[$LAZYSHELL_TEST_LEAK]\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, other, "leak=[]")
}

// The phase 6 decision: the command is typed into the shell, not exec'd in its
// place. Both halves matter — it must actually run, and the shell must still be
// there afterwards.
func TestNewWithOptionsInjectsTheCommand(t *testing.T) {
	m := newTestManager(t)

	sess, err := m.NewWithOptions(Options{
		Name:    "api",
		Shell:   testShell,
		Command: "echo commande-lancee",
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	waitForScreen(t, sess, "commande-lancee")

	if got := sess.Status(); got != StatusRunning {
		t.Fatalf("Status after the command finished = %v, want running", got)
	}

	// And the shell is still usable, which is the reason for injecting rather
	// than exec'ing in the first place.
	if _, err := sess.Write([]byte("echo toujours-la\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "toujours-la")
}

func TestNewWithOptionsUsesCwd(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()

	sess, err := m.NewWithOptions(Options{
		Name:    "api",
		Shell:   testShell,
		Cwd:     dir,
		Command: "pwd",
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if sess.Cwd != dir {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, dir)
	}

	// The pty wraps at 80 columns and t.TempDir() paths are long, so match on
	// the final element rather than on the whole path.
	waitForScreen(t, sess, filepath.Base(dir))
}

func TestNewWithOptionsEmptyCwdIsTheProcessDir(t *testing.T) {
	m := newTestManager(t)

	sess, err := m.NewWithOptions(Options{Name: "t", Shell: testShell})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if sess.Cwd == "" {
		t.Error("Cwd is empty, want the process's working directory")
	}
}

// New and NewInDir are wrappers now; they must keep behaving exactly as before.
func TestPositionalConstructorsStillWork(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()

	sess, err := m.NewInDir("in-dir", testShell, dir)
	if err != nil {
		t.Fatalf("NewInDir: %v", err)
	}

	if sess.Name() != "in-dir" || sess.Cwd != dir {
		t.Errorf("NewInDir gave name=%q cwd=%q, want in-dir/%s", sess.Name(), sess.Cwd, dir)
	}

	plain := newTestSession(t, m, "plain")

	if plain.Name() != "plain" || plain.Status() != StatusRunning {
		t.Errorf("New gave name=%q status=%v, want plain/running", plain.Name(), plain.Status())
	}
}

// A ".env" sitting in the session's cwd is loaded automatically.
func TestNewWithOptionsLoadsDefaultDotEnv(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	writeDotEnv(t, filepath.Join(dir, ".env"), "LAZYSHELL_TEST_PORT=4000\n")

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Cwd: dir})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo port=$LAZYSHELL_TEST_PORT\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "port=4000")
}

// NoDefaultEnvFile skips the automatic ".env" lookup for that session only.
func TestNewWithOptionsNoDefaultEnvFileSkipsIt(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	writeDotEnv(t, filepath.Join(dir, ".env"), "LAZYSHELL_TEST_PORT=4000\n")

	skip := true
	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Cwd: dir, NoDefaultEnvFile: &skip})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo port=[$LAZYSHELL_TEST_PORT]\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "port=[]")
}

// Manager.DisableDefaultEnv turns off the automatic lookup for every session,
// and a session's own NoDefaultEnvFile=false brings it back for that one.
func TestManagerDisableDefaultEnvOverriddenPerSession(t *testing.T) {
	m := newTestManager(t)
	m.DisableDefaultEnv = true
	dir := t.TempDir()
	writeDotEnv(t, filepath.Join(dir, ".env"), "LAZYSHELL_TEST_PORT=4000\n")

	load := false
	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell, Cwd: dir, NoDefaultEnvFile: &load})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo port=$LAZYSHELL_TEST_PORT\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "port=4000")
}

// EnvFiles are loaded in order, a later file overriding an earlier one, and
// Env always wins over all of them.
func TestNewWithOptionsEnvFilesLayerInOrder(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.env")
	second := filepath.Join(dir, "second.env")
	writeDotEnv(t, first, "LAZYSHELL_TEST_A=from-first\nLAZYSHELL_TEST_B=from-first\n")
	writeDotEnv(t, second, "LAZYSHELL_TEST_B=from-second\n")

	sess, err := m.NewWithOptions(Options{
		Name:     "api",
		Shell:    testShell,
		Cwd:      dir,
		EnvFiles: []string{first, second},
		Env:      map[string]string{"LAZYSHELL_TEST_B": "from-env"},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo a=$LAZYSHELL_TEST_A b=$LAZYSHELL_TEST_B\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "a=from-first b=from-env")
}

// A missing explicit env file is an error, unlike a missing default ".env".
func TestNewWithOptionsMissingEnvFileIsAnError(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.NewWithOptions(Options{
		Name:     "api",
		Shell:    testShell,
		EnvFiles: []string{filepath.Join(t.TempDir(), "nope.env")},
	}); err == nil {
		t.Fatal("NewWithOptions with a missing env file: want error, got nil")
	}
}

// Manager.DefaultEnvFiles (the --env-file flag) applies to every session.
func TestManagerDefaultEnvFilesApplyToEverySession(t *testing.T) {
	m := newTestManager(t)
	path := filepath.Join(t.TempDir(), "global.env")
	writeDotEnv(t, path, "LAZYSHELL_TEST_PORT=5000\n")
	m.DefaultEnvFiles = []string{path}

	sess, err := m.NewWithOptions(Options{Name: "api", Shell: testShell})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	if _, err := sess.Write([]byte("echo port=$LAZYSHELL_TEST_PORT\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForScreen(t, sess, "port=5000")
}

// TERM is the environment variable a user might need to lower: some programs
// behave better when told the terminal can do less than it actually can.
func TestManagerTermReachesTheChildEnvironment(t *testing.T) {
	m := NewManager()

	if got := m.term(); got != DefaultTerm {
		t.Errorf("term() = %q, want the default %q", got, DefaultTerm)
	}

	m.Term = "xterm"

	env, err := buildEnv(m, "session-id", "/tmp/session-id.sock", t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	var found string
	for _, entry := range env {
		if after, ok := strings.CutPrefix(entry, "TERM="); ok {
			found = after
		}
	}

	if found != "xterm" {
		t.Errorf("TERM = %q in the child environment, want the configured %q", found, "xterm")
	}
}
