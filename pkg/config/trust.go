package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// The trust store is the guard rail around the one genuinely risky thing in
// phase 6: a lazyshell.yml is versioned in a repository, so cloning a project
// and running `lazyshell` in it would otherwise execute arbitrary commands with
// no prompt at all. The model is direnv's — approval is asked once per file,
// remembered, and invalidated as soon as the file's content changes.

// TrustPath resolves the trust store's location: $LAZYSHELL_TRUST if set
// (mainly so tests never touch the real home directory, same escape hatch as
// Path()'s $LAZYSHELL_CONFIG), else trust.yml next to the user config.
func TrustPath() string {
	if p := os.Getenv("LAZYSHELL_TRUST"); p != "" {
		return p
	}

	cfg := Path()
	if cfg == "" {
		return ""
	}

	return filepath.Join(filepath.Dir(cfg), "trust.yml")
}

// TrustStore maps a project file's absolute path to the sha256 of the content
// that was approved for it.
type TrustStore map[string]string

// LoadTrust reads the trust store. A missing or unreadable store is not an
// error: it just means nothing is approved yet, which fails closed.
func LoadTrust(path string) TrustStore {
	store := TrustStore{}

	if path == "" {
		return store
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}

	if err := yaml.Unmarshal(data, &store); err != nil {
		return TrustStore{}
	}

	return store
}

// IsTrusted reports whether this exact content, at this exact path, has been
// approved. The hash is over the content, not the path alone, so any edit to
// the file asks again — which is the whole point: `git pull` can change what a
// project file launches.
func IsTrusted(path string, content []byte) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	return LoadTrust(TrustPath())[abs] == hashContent(content)
}

// Trust records this content, at this path, as approved.
func Trust(path string, content []byte) error {
	storePath := TrustPath()
	if storePath == "" {
		return fmt.Errorf("config: aucun emplacement pour le fichier de confiance")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	store := LoadTrust(storePath)
	store[abs] = hashContent(content)

	data, err := yaml.Marshal(store)
	if err != nil {
		return fmt.Errorf("config: sérialisation de %s: %w", storePath, err)
	}

	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		return fmt.Errorf("config: création de %s: %w", filepath.Dir(storePath), err)
	}

	// Written through a temporary file in the same directory then renamed: a
	// half-written trust store would either lose every past approval or, worse,
	// be unparseable and silently reset to empty (LoadTrust fails closed).
	tmp, err := os.CreateTemp(filepath.Dir(storePath), ".trust-*.yml")
	if err != nil {
		return fmt.Errorf("config: écriture de %s: %w", storePath, err)
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()

		return fmt.Errorf("config: écriture de %s: %w", storePath, err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()

		return fmt.Errorf("config: écriture de %s: %w", storePath, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: écriture de %s: %w", storePath, err)
	}

	if err := os.Rename(tmp.Name(), storePath); err != nil {
		return fmt.Errorf("config: écriture de %s: %w", storePath, err)
	}

	return nil
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}
