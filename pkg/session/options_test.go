package session

import (
	"path/filepath"
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
