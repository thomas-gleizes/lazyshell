package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// Groups declares the session groups this project uses, and — this is the
	// part that matters — the order their headers appear in the sessions
	// panel. Declaring a group is optional: a session may name a group that is
	// not listed here, and it simply sorts after the declared ones.
	//
	// A group carries a name and a label, and deliberately nothing else. Per
	// this struct's doc comment above, a project file says what exists; how
	// the user's interface renders it is not a repository's business.
	Groups []GroupSpec `yaml:"groups"`
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

// GroupSpec is one declared group, as written in the file.
//
// A name and nothing else, deliberately. A separate display label was
// considered and dropped: the name is already the string the panel shows, and
// a second field saying the same thing is exactly the speculative surface this
// file's whitelist exists to keep out.
type GroupSpec struct {
	Name string `yaml:"name"`
}

// SessionSpec is one declared session, as written in the file.
type SessionSpec struct {
	Name string `yaml:"name"`
	// Group is the group this session starts in, "" for none. It need not be
	// declared in Groups; that block only fixes the display order.
	Group   string            `yaml:"group"`
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
	// Watch declares this session's pattern watchers: a regex evaluated
	// against each output line, and whether a match notifies. Same
	// GroupSpec precedent as the rest of this file — what exists, not what
	// the interface looks like.
	Watch []WatchSpec `yaml:"watch"`
	// Restart is this session's automatic restart policy, as written:
	// "never" (or empty), "on-failure", or "always". See resolveRestartPolicy
	// for what happens to anything else.
	Restart string `yaml:"restart"`
}

// WatchSpec is one pattern watcher, as written in the file.
type WatchSpec struct {
	Pattern string `yaml:"pattern"`
	Notify  bool   `yaml:"notify"`
}

// RestartPolicy is a session's validated automatic restart policy.
// RestartNever is the empty string deliberately: it is Go's zero value, so
// every existing session.Options{} literal — every test, every session not
// declared in a project file — keeps meaning "never restart" with no
// explicit opt-out required.
type RestartPolicy string

const (
	RestartNever     RestartPolicy = ""
	RestartOnFailure RestartPolicy = "on-failure"
	RestartAlways    RestartPolicy = "always"
)

// ResolvedSession is a SessionSpec that passed validation, with its working
// directory made absolute. This — not SessionSpec — is what session creation
// consumes, so an unresolved relative path cannot reach a pty by accident.
type ResolvedSession struct {
	Name string
	// Group is the session's group, trimmed; "" for an ungrouped session.
	Group   string
	Cwd     string
	Command string
	Env     map[string]string
	// EnvFiles is the project's EnvFiles followed by the session's own,
	// already resolved to absolute paths.
	EnvFiles []string
	// NoDefaultEnv is the session's NoDefaultEnv if set, else the project's.
	// Nil means neither said anything — defer to the Manager's own setting.
	NoDefaultEnv *bool
	// Watch is the session's pattern watchers, already checked for a valid
	// regexp — see Validate, which drops (and reports) any entry that fails
	// to compile rather than letting a typo cost the whole session.
	Watch []WatchSpec
	// Restart is the session's automatic restart policy, already checked —
	// see resolveRestartPolicy. An unrecognized value falls back to
	// RestartNever with a warning, rather than dropping the session: unlike
	// a bad watch pattern, a bad restart policy is a single scalar with a
	// safe default, not a list of independent entries.
	Restart RestartPolicy
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

		group, err := resolveGroupName(spec.Group)
		if err != nil {
			errs = append(errs, fmt.Errorf("session %q: %w", name, err))

			continue
		}

		noDefaultEnv := spec.NoDefaultEnv
		if noDefaultEnv == nil {
			noDefaultEnv = p.NoDefaultEnv
		}

		watch, watchErrs := validateWatch(name, spec.Watch)
		errs = append(errs, watchErrs...)

		restart, restartErr := resolveRestartPolicy(name, spec.Restart)
		if restartErr != nil {
			errs = append(errs, restartErr)
		}

		seen[name] = true
		resolved = append(resolved, ResolvedSession{
			Name:         name,
			Group:        group,
			Cwd:          cwd,
			Command:      spec.Command,
			Env:          spec.Env,
			EnvFiles:     resolveEnvFilePaths(dir, append(append([]string{}, p.EnvFiles...), spec.EnvFiles...)),
			NoDefaultEnv: noDefaultEnv,
			Watch:        watch,
			Restart:      restart,
		})
	}

	return resolved, errs
}

// validateWatch compile-checks each of a session's declared watchers. An
// invalid pattern is dropped and reported, exactly like a bad group entry —
// it costs only that one watcher, never the session it belongs to or the
// other watchers declared alongside it.
func validateWatch(sessionName string, specs []WatchSpec) ([]WatchSpec, []error) {
	var (
		valid []WatchSpec
		errs  []error
	)

	for _, spec := range specs {
		if _, err := regexp.Compile(spec.Pattern); err != nil {
			errs = append(errs, fmt.Errorf("session %q: motif de veille %q invalide: %w", sessionName, spec.Pattern, err))

			continue
		}

		valid = append(valid, spec)
	}

	return valid, errs
}

// resolveRestartPolicy validates a session's raw restart: value. Unlike
// validateWatch, an unrecognized value does not cost the entry it belongs
// to: it falls back to RestartNever and reports why, the same "bad scalar,
// safe default" treatment Config.Validate gives an unknown language — a
// project file is hand-edited and read at startup, and a typo here must
// never turn off the session it's attached to.
func resolveRestartPolicy(sessionName, raw string) (RestartPolicy, error) {
	switch RestartPolicy(strings.TrimSpace(raw)) {
	case RestartNever, "never":
		return RestartNever, nil
	case RestartOnFailure:
		return RestartOnFailure, nil
	case RestartAlways:
		return RestartAlways, nil
	default:
		return RestartNever, fmt.Errorf(
			"session %q: restart %q inconnu (attendu : never, on-failure, always), retour à never",
			sessionName, raw)
	}
}

// ResolvedGroups returns the declared group names, trimmed, in declaration
// order — which is the order the sessions panel draws their headers in. Bad
// entries are dropped and reported, exactly like Validate's bad sessions: a
// hand-edited file must never cost the user the groups that were fine.
//
// Kept separate from Validate rather than folded into it because the two
// answer different questions and have different consumers: Validate produces
// sessions to start, this produces a display order. Their errors are joined
// by the caller.
func (p ProjectConfig) ResolvedGroups() ([]string, []error) {
	var (
		groups []string
		errs   []error
	)

	seen := make(map[string]bool, len(p.Groups))

	for i, spec := range p.Groups {
		name, err := resolveGroupName(spec.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("groupe #%d: %w", i+1, err))

			continue
		}

		if name == "" {
			errs = append(errs, fmt.Errorf("groupe #%d: nom vide", i+1))

			continue
		}

		if seen[name] {
			errs = append(errs, fmt.Errorf("groupe %q: nom en double", name))

			continue
		}

		seen[name] = true
		groups = append(groups, name)
	}

	return groups, errs
}

// resolveGroupName trims a group name and rejects the one shape that would
// actually break something: a name spanning several lines, which would tear
// the panel's single-line header in two and desynchronise the row model from
// what is on screen. An empty name is not an error here — it is how a session
// says "no group" — so callers that do require one check for it themselves.
func resolveGroupName(group string) (string, error) {
	name := strings.TrimSpace(group)

	if strings.ContainsAny(name, "\n\r") {
		return "", fmt.Errorf("groupe %q: saut de ligne interdit dans un nom de groupe", group)
	}

	return name, nil
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
