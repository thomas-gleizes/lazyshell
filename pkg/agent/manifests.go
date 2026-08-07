package agent

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed manifests/*.yml
var builtinManifests embed.FS

// LoadManifests builds a Detector from the manifests shipped in the binary,
// overridden or extended by whatever is in overrideDir (typically
// config.AgentsDir()). A file in overrideDir whose base name matches a
// built-in manifest (e.g. "claude.yml") replaces it outright — manifests are
// ordered rule lists, so merging them field by field would be ambiguous
// about which side's ordering wins; a different base name adds a manifest
// for another agent. A missing overrideDir is not an error: the built-ins
// alone are what makes this phase work with zero configuration.
//
// A manifest that fails to parse is skipped, not fatal — one broken file
// degrades detection for that one agent, never the whole feature. Every
// skip is returned as a warning for the caller to print to stderr, the same
// contract pkg/config's Load/Validate use.
func LoadManifests(overrideDir string) (*Detector, []error) {
	manifests := map[string]Manifest{}
	var warnings []error

	entries, err := builtinManifests.ReadDir("manifests")
	if err != nil {
		// Cannot happen outside a build that broke the embed, but degrade
		// rather than panic if it ever does.
		warnings = append(warnings, fmt.Errorf("agent: built-in manifests: %w", err))
	}

	for _, entry := range entries {
		data, err := builtinManifests.ReadFile(filepath.Join("manifests", entry.Name()))
		if err != nil {
			warnings = append(warnings, fmt.Errorf("agent: built-in manifest %s: %w", entry.Name(), err))
			continue
		}

		m, err := parseManifest(data)
		if err != nil {
			warnings = append(warnings, err)
			continue
		}

		manifests[strings.ToLower(m.Process)] = m
	}

	if overrideDir != "" {
		overrides, warns := loadOverrideManifests(overrideDir)
		warnings = append(warnings, warns...)

		for process, m := range overrides {
			manifests[process] = m
		}
	}

	return NewDetector(manifests), warnings
}

// loadOverrideManifests scans dir for *.yml files. A missing directory is
// not an error — most installs never create one.
func loadOverrideManifests(dir string) (map[string]Manifest, []error) {
	manifests := map[string]Manifest{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return manifests, nil
		}

		return manifests, []error{fmt.Errorf("agent: read %s: %w", dir, err)}
	}

	var warnings []error

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("agent: read %s: %w", path, err))
			continue
		}

		m, err := parseManifest(data)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("agent: %s: %w", path, err))
			continue
		}

		manifests[strings.ToLower(m.Process)] = m
	}

	return manifests, warnings
}
