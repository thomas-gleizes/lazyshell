package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// The state file is a recording, not a declaration: at exit, lazyshell writes
// down what was actually running in this directory — name, group, cwd,
// command — and offers to restore it next time, but only when no
// lazyshell.yml is present (see RestoreLayout). Unlike a project file it never
// goes through the trust store: every command it can contain already ran once
// under this same account, nothing new is being granted. Its only guard is the
// file's own permissions — see SaveState/LoadState.

// StateDir is where saved layouts live: $XDG_CONFIG_HOME/lazyshell/state,
// else ~/.config/lazyshell/state — a subdirectory of configDir, so
// `~/.config/lazyshell` stays the one place to know about.
func StateDir() string {
	dir := configDir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "state")
}

// StatePath returns the file a given working directory's layout is saved to:
// StateDir joined with the sha256 hex of cwd's absolute, cleaned form. Two
// different directories never collide; the same directory always resolves to
// the same file regardless of a trailing slash or how it was reached.
func StatePath(cwd string) (string, error) {
	dir := StateDir()
	if dir == "" {
		return "", fmt.Errorf("config: emplacement de l'état introuvable (pas de dossier personnel)")
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("config: résolution de %q: %w", cwd, err)
	}

	sum := sha256.Sum256([]byte(filepath.Clean(abs)))

	return filepath.Join(dir, hex.EncodeToString(sum[:])+".yml"), nil
}

// StateFile is the on-disk record of one directory's session layout.
type StateFile struct {
	// Path is the cwd this layout was saved for, in clear — the filename is
	// its hash, so this is what makes a directory listing legible.
	Path    string    `yaml:"path"`
	SavedAt time.Time `yaml:"saved_at"`
	// Sessions are in the order they appeared in the sessions panel.
	Sessions []StateSession `yaml:"sessions"`
}

// StateSession is one session's saved recipe: exactly the fields the roadmap
// scoped this feature to. Deliberately narrower than SessionSpec — Env,
// EnvFiles, Watch, Restart and Locked are project-declaration concerns that go
// through the trust store, and this file never does.
type StateSession struct {
	Name    string `yaml:"name"`
	Group   string `yaml:"group,omitempty"`
	Cwd     string `yaml:"cwd"`
	Command string `yaml:"command,omitempty"`
}

// SaveState writes cwd's layout, overwriting whatever was there before.
// Written through a temporary file in the same directory then renamed, with
// mode 0600 before any content reaches it — the same atomic-write shape
// Trust uses for trust.yml, and for the same reason: a state file lists
// commands, and a half-written or world-readable one is worse than none.
func SaveState(cwd string, sessions []StateSession) error {
	path, err := StatePath(cwd)
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}

	data, err := yaml.Marshal(StateFile{Path: abs, SavedAt: time.Now(), Sessions: sessions})
	if err != nil {
		return fmt.Errorf("config: sérialisation de %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: création de %s: %w", filepath.Dir(path), err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.yml")
	if err != nil {
		return fmt.Errorf("config: écriture de %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()

		return fmt.Errorf("config: écriture de %s: %w", path, err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()

		return fmt.Errorf("config: écriture de %s: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: écriture de %s: %w", path, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("config: écriture de %s: %w", path, err)
	}

	return nil
}

// LoadState reads cwd's saved layout. A missing file is not an error — it
// returns (nil, nil), the same "nothing to report" idiom ProjectPath's ""
// return already establishes elsewhere in this package. A file whose
// permissions are wider than the 0600 SaveState writes is treated the same
// way: silently ignored rather than trusted, since anything on the machine
// could have altered it once it stopped being owner-only.
func LoadState(cwd string) (*StateFile, error) {
	path, err := StatePath(cwd)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("config: lecture de %s: %w", path, err)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: lecture de %s: %w", path, err)
	}

	var state StateFile
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("config: analyse de %s: %w", path, err)
	}

	return &state, nil
}
