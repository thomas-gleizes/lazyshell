package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeDotEnv(t, path, `# a comment
PORT=3000

export FOO=bar
QUOTED="with spaces"
SINGLE='also quoted'
EMPTY=
`)

	vars, err := parseDotEnvFile(path)
	if err != nil {
		t.Fatalf("parseDotEnvFile: %v", err)
	}

	want := map[string]string{
		"PORT":   "3000",
		"FOO":    "bar",
		"QUOTED": "with spaces",
		"SINGLE": "also quoted",
		"EMPTY":  "",
	}

	for k, v := range want {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
	if len(vars) != len(want) {
		t.Errorf("vars = %v, want exactly %v", vars, want)
	}
}

func TestParseDotEnvFileRejectsInvalidLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeDotEnv(t, path, "not-a-valid-line\n")

	if _, err := parseDotEnvFile(path); err == nil {
		t.Fatal("parseDotEnvFile on an invalid line: want error, got nil")
	}
}

func TestParseDotEnvFileMissing(t *testing.T) {
	if _, err := parseDotEnvFile(filepath.Join(t.TempDir(), "nope.env")); err == nil {
		t.Fatal("parseDotEnvFile on a missing file: want error, got nil")
	}
}

func TestLoadDotEnvFileIfExistsMissingIsNotAnError(t *testing.T) {
	vars, err := loadDotEnvFileIfExists(filepath.Join(t.TempDir(), "nope.env"))
	if err != nil {
		t.Fatalf("loadDotEnvFileIfExists on a missing file: %v, want nil", err)
	}
	if len(vars) != 0 {
		t.Errorf("vars = %v, want none", vars)
	}
}

func writeDotEnv(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
