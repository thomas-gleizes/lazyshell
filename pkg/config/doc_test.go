package config

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The README section the reference table and the example live in. Anchored on
// the headings rather than on line numbers, so the test survives edits above it.
const (
	readmePath      = "../../README.md"
	referenceHeader = "### Reference"
	exampleHeader   = "### Example"
	nextSectionMark = "\n## "
)

// TestEveryOptionIsDocumented is the mechanism that keeps the README from
// falling behind the code. Adding a field to Config without a line in the
// reference table — or leaving a line behind after removing a field — fails the
// build.
//
// It compares key *names*, not text: the README is in English and the template
// `lazyshell config init` writes is in French, so a verbatim comparison would
// force one of the two into the wrong language. Names are what a user searches
// for and what actually goes stale.
func TestEveryOptionIsDocumented(t *testing.T) {
	readme := readREADME(t)

	documented := keysInTable(t, section(t, readme, referenceHeader))
	actual := configKeys()

	assertSameKeys(t, "reference table", documented, actual)
}

// TestExampleCoversEveryOption checks the second half of the same promise: the
// example block is advertised as "every option at its default value", so a
// missing key there is a broken promise even when the table is complete.
func TestExampleCoversEveryOption(t *testing.T) {
	readme := readREADME(t)

	documented := keysInYAML(section(t, readme, exampleHeader))
	actual := configKeys()

	for key := range actual {
		if !documented[key] {
			t.Errorf("config key %q is missing from the README's example block, which claims to show every option", key)
		}
	}

	// The reverse direction needs the Go type to arbitrate, because YAML alone
	// cannot: `theme:` and `keybindings:` are both a name followed by an
	// indented block, but theme's children are options and keybindings' are
	// action ids. So a documented name is extra unless it is either a struct
	// parent (a prefix of a real option) or an entry of a map option.
	for key := range documented {
		if actual[key] || isParentOf(key, actual) || isEntryOfMap(key, actual) {
			continue
		}

		t.Errorf("the README's example block has %q, which is not a config option", key)
	}
}

// isParentOf reports whether key is the parent of at least one real option —
// "theme", given "theme.selected_bg_color".
func isParentOf(key string, actual map[string]bool) bool {
	for candidate := range actual {
		if strings.HasPrefix(candidate, key+".") {
			return true
		}
	}

	return false
}

// isEntryOfMap reports whether key is a child of a real leaf option, which can
// only be a map: "keybindings.new_session" under "keybindings".
func isEntryOfMap(key string, actual map[string]bool) bool {
	parent, _, found := strings.Cut(key, ".")

	return found && actual[parent]
}

// TestREADMEExampleLoadsToDefaults goes one step further than name matching:
// the documented example must actually parse, must pass our own validation, and
// must produce the built-in defaults. An example whose values have drifted would
// silently reconfigure everyone who copies it — the worst possible failure mode
// for a block whose whole promise is "this is what you already have".
//
// Keybindings and Theme are compared elsewhere, in pkg/gui: their real defaults
// live there (bindings(), defaultTheme()) and Config's zero values for them mean
// "keep whatever pkg/gui decided", which is why they are empty in Default() but
// spelled out in the example.
func TestREADMEExampleLoadsToDefaults(t *testing.T) {
	block := yamlBlock(t, section(t, readREADME(t), exampleHeader))

	cfg, err := Load(writeTemp(t, block))
	if err != nil {
		t.Fatalf("Load(README example) = %v, want no error", err)
	}

	if len(cfg.Warnings) > 0 {
		t.Errorf("README example produced warnings %v, want none", cfg.Warnings)
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Errorf("README example failed validation: %v", errs)
	}

	if len(cfg.Keybindings) == 0 || cfg.Theme == (Theme{}) {
		t.Error("README example leaves keybindings or theme empty, but it claims to show every option at its default")
	}

	cfg.Warnings, cfg.Keybindings, cfg.Theme = nil, nil, Theme{}

	want := Default()
	want.Keybindings, want.Theme = nil, Theme{}

	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("README example loads to\n%+v\nwant the defaults\n%+v", cfg, want)
	}
}

// configKeys is every leaf option of Config, in dotted notation: a nested
// struct contributes its children, not itself, and a map (keybindings) is a
// leaf since its keys are action ids, not options.
func configKeys() map[string]bool {
	keys := make(map[string]bool)
	collectKeys(reflect.TypeOf(Config{}), "", keys)

	return keys
}

func collectKeys(t reflect.Type, prefix string, out map[string]bool) {
	for i := range t.NumField() {
		field := t.Field(i)

		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}

		full := name
		if prefix != "" {
			full = prefix + "." + name
		}

		if field.Type.Kind() == reflect.Struct {
			collectKeys(field.Type, full, out)

			continue
		}

		out[full] = true
	}
}

// keysInTable pulls the first column out of every row of the markdown table,
// stripping the backticks the README wraps keys in.
func keysInTable(t *testing.T, text string) map[string]bool {
	t.Helper()

	row := regexp.MustCompile("(?m)^\\|\\s*`([a-z0-9_.]+)`\\s*\\|")

	keys := make(map[string]bool)
	for _, match := range row.FindAllStringSubmatch(text, -1) {
		keys[match[1]] = true
	}

	if len(keys) == 0 {
		t.Fatal("no keys found in the README reference table — has its format changed?")
	}

	return keys
}

// keysInYAML reads every name out of the example block, tracking one level of
// nesting through indentation: both `theme` and `theme.selected_bg_color` come
// back. Working out which of those two shapes is the real option is the
// caller's job, since only the Go type knows.
//
// Deliberately a small hand-rolled scanner rather than a YAML parse: a real
// parse would resolve the structure but lose the distinction between a key that
// is present in the file and one that merely exists in the schema, which is
// exactly what this test is about.
func keysInYAML(text string) map[string]bool {
	keys := make(map[string]bool)
	parent := ""

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") {
			continue
		}

		name, rest, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}

		value := strings.TrimSpace(strings.SplitN(rest, "#", 2)[0])

		if strings.HasPrefix(line, " ") {
			if parent != "" {
				keys[parent+"."+name] = true
			}

			continue
		}

		keys[name] = true

		// A name with no value on its line opens a block; anything else ends
		// whichever block was open.
		parent = ""
		if value == "" {
			parent = name
		}
	}

	return keys
}

func assertSameKeys(t *testing.T, what string, documented, actual map[string]bool) {
	t.Helper()

	for key := range actual {
		if !documented[key] {
			t.Errorf("config key %q has no entry in the README's %s — every option must be documented", key, what)
		}
	}

	for key := range documented {
		if !actual[key] {
			t.Errorf("the README's %s documents %q, which Config no longer has", what, key)
		}
	}

	if t.Failed() {
		t.Logf("Config keys: %v", sortedSet(actual))
		t.Logf("documented:  %v", sortedSet(documented))
	}
}

// section returns the README text between header and the next top-level
// heading.
func section(t *testing.T, readme, header string) string {
	t.Helper()

	_, after, found := strings.Cut(readme, header)
	if !found {
		t.Fatalf("README has no %q section", header)
	}

	if end := strings.Index(after, nextSectionMark); end >= 0 {
		after = after[:end]
	}

	// A "### " sibling heading also ends the section.
	if end := strings.Index(after, "\n### "); end >= 0 {
		after = after[:end]
	}

	return after
}

// yamlBlock extracts the content of the first fenced yaml block in text.
func yamlBlock(t *testing.T, text string) string {
	t.Helper()

	_, after, found := strings.Cut(text, "```yaml\n")
	if !found {
		t.Fatal("no ```yaml block found in the README section")
	}

	body, _, found := strings.Cut(after, "```")
	if !found {
		t.Fatal("unterminated ```yaml block in the README")
	}

	return body
}

func readREADME(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading %s: %v", readmePath, err)
	}

	return string(data)
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()

	path := t.TempDir() + "/config.yml"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	return path
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}
