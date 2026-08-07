package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

func TestInitProjectWritesAUsableFile(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	if err := InitProject(dir, &out); err != nil {
		t.Fatalf("InitProject: %v", err)
	}

	path := filepath.Join(dir, "lazyshell.yml")
	if !strings.Contains(out.String(), path) {
		t.Errorf("output = %q, want the created path", out.String())
	}

	// The template is documentation, but it also has to parse and validate — an
	// example that would be rejected on the next launch is worse than none.
	pcfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject on the generated file: %v", err)
	}

	if len(pcfg.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none: the template must only use whitelisted keys", pcfg.Warnings)
	}

	valid, errs := pcfg.Validate()
	if len(errs) != 0 {
		t.Errorf("Validate on the generated file: %v", errs)
	}
	if len(valid) != 2 {
		t.Errorf("len(valid) = %d, want the template's 2 example sessions", len(valid))
	}
}

func TestInitProjectRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lazyshell.yml")

	original := []byte("sessions:\n  - name: precieuse\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := InitProject(dir, io.Discard); err == nil {
		t.Fatal("InitProject over an existing file: want error, got nil")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if !bytes.Equal(after, original) {
		t.Error("InitProject clobbered an existing lazyshell.yml")
	}
}

func TestAllowProjectApprovesWithoutLaunching(t *testing.T) {
	dir := projectDir(t, threeSessions, "services/api", "web")

	var out bytes.Buffer
	if err := AllowProject("", &out); err != nil {
		t.Fatalf("AllowProject: %v", err)
	}

	path := filepath.Join(dir, "lazyshell.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !config.IsTrusted(path, content) {
		t.Error("the file is still untrusted after AllowProject")
	}

	// And the next launch starts without being able to ask anything.
	a := newTestApp(t, Options{}, nonInteractive)
	if got := len(a.sessions.List()); got != 3 {
		t.Errorf("len(sessions) after allow = %d, want 3", got)
	}
}

func TestAllowProjectWithNoFile(t *testing.T) {
	projectDir(t, "")

	if err := AllowProject("", io.Discard); err == nil {
		t.Fatal("AllowProject with no project file: want error, got nil")
	}
}

// PrintAgentHookConfig writes to stdout, never to disk — see its doc
// comment for why (no single right path to merge JSON/TOML into).
func TestPrintAgentHookConfigWritesNoFile(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	if err := PrintAgentHookConfig(&out); err != nil {
		t.Fatalf("PrintAgentHookConfig: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("PrintAgentHookConfig left files behind: %v", entries)
	}

	for _, want := range []string{"lazyshell hook working", "lazyshell hook blocked", "lazyshell hook done", "LAZYSHELL_SOCK"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}
