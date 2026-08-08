package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectFileNames are the file names looked up in the current directory, in
// this order, when no explicit path was given. No parent directory is ever
// searched: the file that gets executed must be the one sitting in the
// directory lazyshell was started from, with no ambiguity about which.
var ProjectFileNames = []string{"lazyshell.yml", ".lazyshell.yml"}

// ProjectConfig is the subset of Config a lazyshell.yml may override, plus the
// sessions it declares. The whitelist is not a filter applied after the fact:
// it is this struct — a key with no field here has nowhere to land. That is
// deliberate. A lazyshell.yml comes from a repository, possibly someone else's,
// and must never be able to remap keybindings or the pass-through prefix under
// the user's fingers.
type ProjectConfig struct {
	// Shell overrides the user configuration's Shell for this project only.
	Shell string `yaml:"shell"`
	// Sessions are started at launch, in this order.
	Sessions []SessionSpec `yaml:"sessions"`
	// EnvFiles are .env-style files loaded, in order, for every session this
	// project declares — before each session's own EnvFiles, and before its
	// inline Env. Relative paths resolve against the project file's
	// directory, like Cwd.
	EnvFiles []string `yaml:"env_files"`
	// NoDefaultEnv disables the automatic "<session cwd>/.env" lookup for
	// every declared session, unless a session's own NoDefaultEnv overrides
	// it back on.
	NoDefaultEnv *bool `yaml:"no_default_env"`

	// Path is the absolute path of the file this was read from. Relative cwds
	// resolve against its directory, not against the process's.
	Path string `yaml:"-"`
	// Raw is the file's content as read, hashed by the trust store so that
	// editing the file asks for approval again.
	Raw []byte `yaml:"-"`
	// Warnings lists the keys that were present but ignored, so that a `theme:`
	// in a project file says why it does nothing instead of being silently
	// dropped.
	Warnings []string `yaml:"-"`
}

// SessionSpec is one declared session, as written in the file.
type SessionSpec struct {
	Name    string            `yaml:"name"`
	Cwd     string            `yaml:"cwd"`
	Command string            `yaml:"command"`
	Env     map[string]string `yaml:"env"`
	// EnvFiles are .env-style files loaded for this session only, after the
	// project's own EnvFiles — a later file, and this list, override a key
	// set by anything earlier. Relative paths resolve against the project
	// file's directory, like Cwd.
	EnvFiles []string `yaml:"env_files"`
	// NoDefaultEnv overrides the project's NoDefaultEnv for this session
	// only. Nil means "use the project's setting".
	NoDefaultEnv *bool `yaml:"no_default_env"`
}

// ResolvedSession is a SessionSpec that passed validation, with its working
// directory made absolute. This — not SessionSpec — is what session creation
// consumes, so an unresolved relative path cannot reach a pty by accident.
type ResolvedSession struct {
	Name    string
	Cwd     string
	Command string
	Env     map[string]string
	// EnvFiles is the project's EnvFiles followed by the session's own,
	// already resolved to absolute paths.
	EnvFiles []string
	// NoDefaultEnv is the session's NoDefaultEnv if set, else the project's.
	// Nil means neither said anything — defer to the Manager's own setting.
	NoDefaultEnv *bool
}

// ProjectPath resolves which project file to read: the --config-file flag, then
// $LAZYSHELL_PROJECT_CONFIG, then ./lazyshell.yml, then ./.lazyshell.yml.
// Returns "" when there is none, which means "behave exactly as before phase 6".
//
// An explicitly requested path (flag or env) is returned even when it does not
// exist: asking for a specific file and getting silence instead of an error is
// worse than the error. The conventional names are only returned when present.
func ProjectPath(flag string) string {
	if flag != "" {
		return expandHome(flag)
	}

	if p := os.Getenv("LAZYSHELL_PROJECT_CONFIG"); p != "" {
		return expandHome(p)
	}

	for _, name := range ProjectFileNames {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			return name
		}
	}

	return ""
}

// LoadProject reads and parses the project file at path. Unlike Load, a missing
// file *is* an error here: ProjectPath only returns a conventional name when it
// exists, so reaching this with a missing file means the user named it
// explicitly.
func LoadProject(path string) (ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	return ParseProject(abs, data)
}

// ParseProject is LoadProject's pure half: everything but the file read, so the
// parsing and whitelist rules can be tested without a filesystem.
func ParseProject(path string, data []byte) (ProjectConfig, error) {
	var pcfg ProjectConfig
	if err := yaml.Unmarshal(data, &pcfg); err != nil {
		return ProjectConfig{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	pcfg.Path = path
	pcfg.Raw = data
	pcfg.Warnings = unknownKeys(data, &ProjectConfig{})

	return pcfg, nil
}

// unknownKeys reports the keys the file contains but probe's type has no field
// for. It re-decodes into probe with KnownFields(true) — which turns exactly
// those into an error — and downgrades that error to a list of warnings, since
// an unusable key must not stop a valid file from being used.
//
// probe must be a pointer to a fresh zero value of the target type; it is
// written to and discarded. Shared by the project file and the user config: the
// two have different schemas but exactly the same "a typo must never be silent"
// requirement.
func unknownKeys(data []byte, probe any) []string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	err := dec.Decode(probe)

	var typeErr *yaml.TypeError
	if err == nil || !errors.As(err, &typeErr) {
		return nil
	}

	var warnings []string
	for _, msg := range typeErr.Errors {
		if strings.Contains(msg, "not found in type") {
			warnings = append(warnings, msg)
		}
	}

	return warnings
}

// Validate resolves and checks every declared session. Invalid entries are
// dropped and reported; the valid ones still start. A project file is edited by
// hand and read at startup — one bad entry must never cost the user the other
// sessions, and must never panic or fail silently.
func (p ProjectConfig) Validate() ([]ResolvedSession, []error) {
	dir := filepath.Dir(p.Path)

	var (
		resolved []ResolvedSession
		errs     []error
	)

	seen := make(map[string]bool, len(p.Sessions))

	for i, spec := range p.Sessions {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			errs = append(errs, fmt.Errorf("session #%d: nom vide", i+1))

			continue
		}

		if seen[name] {
			errs = append(errs, fmt.Errorf("session %q: nom en double", name))

			continue
		}

		cwd, err := spec.ResolveCwd(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("session %q: %w", name, err))

			continue
		}

		noDefaultEnv := spec.NoDefaultEnv
		if noDefaultEnv == nil {
			noDefaultEnv = p.NoDefaultEnv
		}

		seen[name] = true
		resolved = append(resolved, ResolvedSession{
			Name:         name,
			Cwd:          cwd,
			Command:      spec.Command,
			Env:          spec.Env,
			EnvFiles:     resolveEnvFilePaths(dir, append(append([]string{}, p.EnvFiles...), spec.EnvFiles...)),
			NoDefaultEnv: noDefaultEnv,
		})
	}

	return resolved, errs
}

// ResolveCwd turns the spec's Cwd into an absolute, existing directory. It is
// resolved against configDir — the directory holding the project file — not
// against the process's working directory, so that a `cwd: ./services/api`
// means the same thing however lazyshell was invoked. An empty Cwd is
// configDir itself.
func (s SessionSpec) ResolveCwd(configDir string) (string, error) {
	cwd := expandHome(strings.TrimSpace(s.Cwd))

	switch {
	case cwd == "":
		cwd = configDir
	case !filepath.IsAbs(cwd):
		cwd = filepath.Join(configDir, cwd)
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("cwd %q: %w", s.Cwd, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("cwd %q: %w", s.Cwd, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q: n'est pas un répertoire", s.Cwd)
	}

	return abs, nil
}

// resolveEnvFilePaths makes each path absolute against dir — the project
// file's directory — exactly like ResolveCwd, but without checking that the
// file exists: a missing explicit env file is reported by session creation
// instead (pkg/session.buildEnv), which still lets a project's other
// sessions start, the same way a bad SessionSpec does not cost the rest.
func resolveEnvFilePaths(dir string, paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		p = expandHome(strings.TrimSpace(p))
		if p != "" && !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}

		resolved = append(resolved, p)
	}

	return resolved
}

// MergeProject applies the project file on top of the user configuration.
// Shell is the only field it can touch — see ProjectConfig's doc comment for
// why the rest is off limits.
func (c Config) MergeProject(p ProjectConfig) Config {
	if p.Shell != "" {
		c.Shell = p.Shell
	}

	return c
}

// expandHome expands a leading ~ to the user's home directory. A ~ that cannot
// be resolved is left as-is rather than turned into an error: the caller's
// os.Stat will produce a better message than this function could.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}

	if p == "~" {
		return home
	}

	return filepath.Join(home, p[2:])
}
