// Command gen-config-schema emits site/assets/config-schema.json: a
// structural description of Config and ProjectConfig/SessionSpec/WatchSpec/
// GroupSpec (dotted key, widget type, default, bounds, closed enum values)
// plus their bilingual help text, read straight from README.md's and
// docs/README.fr.md's own reference tables rather than a hand-authored
// fourth copy.
//
// It is the data behind site/assets/config-editor.js's form — see
// ROADMAP.md's "Éditeur de configuration graphique sur le site statique".
// Run via `go generate ./...` (see the //go:generate directive in
// pkg/config/doc.go) from the repository root: it reads README.md and
// docs/README.fr.md, and writes site/assets/config-schema.json, both as
// relative paths.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/gui"
)

// field is one entry of the generated schema — a leaf option (config side)
// or a declared key at any depth (project side, where "sessions[].watch[].pattern"
// is as real a key as "shell").
type field struct {
	// Key is the dotted config key, "markers.agent_working_color" or, on the
	// project side, "sessions[].restart" — the same "[]" convention
	// README.md's "### Field reference" table already uses for a slice of
	// structs.
	Key string `json:"key"`
	// Type is the widget discriminant config-editor.js switches on: string,
	// int, bool, enum, color, map, or list.
	Type string `json:"type"`
	// Default is the real zero-config value (config.Default(), reflected)
	// for a Config field. Project fields carry no equivalent — a
	// lazyshell.yml has no "unset" value the way Config does — so this stays
	// unset there in favor of DefaultText.
	Default any `json:"default,omitempty"`
	// DefaultTextEN/DefaultTextFR are the project side's Default column,
	// verbatim from README.md/docs/README.fr.md, for defaults that are not a
	// plain value ("this file's own directory", "true if command: is
	// declared, else false") and so cannot round-trip through JSON the way
	// Default does.
	DefaultTextEN string `json:"defaultText_en,omitempty"`
	DefaultTextFR string `json:"defaultText_fr,omitempty"`
	Min           *int   `json:"min,omitempty"`
	Max           *int   `json:"max,omitempty"`
	// Enum lists a closed set of valid values — language, restore_layout,
	// sessions[].restart.
	Enum []string `json:"enum,omitempty"`
	// MapKeyOptions offers a closed choice for a map field's key column
	// (keybindings' action ids) instead of free text. Absent means the key
	// itself is free text too (sessions[].env).
	MapKeyOptions []string `json:"mapKeyOptions,omitempty"`
	// Nilable marks a project field backed by a Go pointer (*bool): "not
	// declared" is itself a meaningful third state, distinct from both true
	// and false, that config-editor.js's form must be able to represent.
	Nilable bool `json:"nilable,omitempty"`
	// ItemType is "string" or "struct", for Type == "list".
	ItemType string `json:"itemType,omitempty"`
	// ItemFields is the field list for one list entry, when ItemType ==
	// "struct" — env_files has none (ItemType == "string"), sessions does.
	ItemFields []field `json:"itemFields,omitempty"`
	HelpEN     string  `json:"help_en,omitempty"`
	HelpFR     string  `json:"help_fr,omitempty"`
}

// schema is config-schema.json's top-level shape.
type schema struct {
	// Config is Config's fields, flattened (a nested struct contributes its
	// children, not itself) — the same shape pkg/config/doc_test.go's
	// collectKeys already walks the struct in, just emitted instead of kept
	// in memory for one test.
	Config []field `json:"config"`
	// Project is ProjectConfig's fields, kept as a tree: unlike Config, a
	// project file's "sessions" is a list of structs, and config-editor.js
	// needs that shape to render one form section per session entry.
	Project []field `json:"project"`
	// ColorNames hints the ANSI names a color field accepts, alongside any
	// W3C name or "#rrggbb" — see gui.ColorNameHints.
	ColorNames []string `json:"colorNames"`
	// ActionIDs is every remappable action id — see gui.ActionIDs.
	ActionIDs []string `json:"actionIds"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-config-schema:", err)
		os.Exit(1)
	}
}

func run() error {
	readmeEN, err := os.ReadFile("README.md")
	if err != nil {
		return fmt.Errorf("reading README.md (run from the repository root): %w", err)
	}

	readmeFR, err := os.ReadFile("docs/README.fr.md")
	if err != nil {
		return fmt.Errorf("reading docs/README.fr.md: %w", err)
	}

	configTableEN := parseTable(section(string(readmeEN), "### Reference"))
	configTableFR := parseTable(section(string(readmeFR), "### Référence"))
	projectTableEN := parseTable(section(string(readmeEN), "### Field reference"))
	projectTableFR := parseTable(section(string(readmeFR), "### Référence des champs"))

	bounds := make(map[string]config.NumericBound, len(config.NumericBounds))
	for _, b := range config.NumericBounds {
		bounds[b.Key] = b
	}

	languages := make([]string, 0, len(config.Languages))
	for lang := range config.Languages {
		languages = append(languages, lang)
	}
	sort.Strings(languages)

	actionIDs := gui.ActionIDs()

	out := schema{
		Config: buildConfigFields(
			reflect.TypeOf(config.Config{}), reflect.ValueOf(config.Default()), "",
			configTableEN, configTableFR, bounds, languages, actionIDs,
		),
		Project: buildProjectFields(
			reflect.TypeOf(config.ProjectConfig{}), "",
			projectTableEN, projectTableFR,
		),
		ColorNames: gui.ColorNameHints(),
		ActionIDs:  actionIDs,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling schema: %w", err)
	}

	data = append(data, '\n')

	const outPath = "site/assets/config-schema.json"
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	return nil
}

// buildConfigFields walks Config (t, with a matching config.Default() in v)
// depth-first, flattening nested structs (Markers, Theme, ...) into dotted
// keys exactly like pkg/config/doc_test.go's collectKeys — the two must
// never disagree about what a "config key" is.
func buildConfigFields(
	t reflect.Type, v reflect.Value, prefix string,
	helpEN, helpFR map[string]tableRow,
	bounds map[string]config.NumericBound,
	languages, actionIDs []string,
) []field {
	var out []field

	for i := range t.NumField() {
		sf := t.Field(i)

		name, _, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}

		key := name
		if prefix != "" {
			key = prefix + "." + name
		}

		fv := v.Field(i)

		if sf.Type.Kind() == reflect.Struct {
			out = append(out, buildConfigFields(sf.Type, fv, key, helpEN, helpFR, bounds, languages, actionIDs)...)

			continue
		}

		f := field{Key: key, HelpEN: helpEN[key].Effect, HelpFR: helpFR[key].Effect}

		switch {
		case key == "language":
			f.Type, f.Enum, f.Default = "enum", languages, fv.String()
		case key == "restore_layout":
			f.Type, f.Enum, f.Default = "enum", config.RestoreLayoutValues, fv.String()
		case key == "keybindings":
			f.Type, f.MapKeyOptions, f.Default = "map", actionIDs, map[string]string{}
		case strings.HasSuffix(name, "_color"):
			f.Type, f.Default = "color", fv.String()
		case sf.Type.Kind() == reflect.Bool:
			f.Type, f.Default = "bool", fv.Bool()
		case sf.Type.Kind() == reflect.Int:
			f.Type, f.Default = "int", int(fv.Int())
			if b, ok := bounds[key]; ok {
				minimum, maximum := b.Min, b.Max
				f.Min = &minimum
				if maximum != 0 {
					f.Max = &maximum
				}
			}
		default:
			f.Type, f.Default = "string", fv.String()
		}

		out = append(out, f)
	}

	return out
}

// buildProjectFields walks a project-side struct (ProjectConfig, then
// recursively SessionSpec/WatchSpec/GroupSpec for a slice-of-struct field),
// keyed the same "parent[].child" way README.md's "### Field reference"
// table already writes a slice-of-struct's entries — see field.Key's doc
// comment. Unlike buildConfigFields there is no live default value to read:
// a lazyshell.yml has no equivalent of Config's zero-config Default(), so
// DefaultText (README's own Default column, per language) is what
// config-editor.js shows instead.
func buildProjectFields(t reflect.Type, prefix string, helpEN, helpFR map[string]tableRow) []field {
	var out []field

	for i := range t.NumField() {
		sf := t.Field(i)

		name, _, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}

		key := name
		if prefix != "" {
			key = prefix + "." + name
		}

		ft := sf.Type

		nilable := false
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
			nilable = true
		}

		f := field{
			Key: key, Nilable: nilable,
			HelpEN: helpEN[key].Effect, HelpFR: helpFR[key].Effect,
			DefaultTextEN: helpEN[key].Default, DefaultTextFR: helpFR[key].Default,
		}

		switch {
		case key == "sessions[].restart":
			f.Type, f.Enum = "enum", config.RestartPolicyValues
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			f.Type, f.ItemType = "list", "struct"
			f.ItemFields = buildProjectFields(ft.Elem(), key+"[]", helpEN, helpFR)
		case ft.Kind() == reflect.Slice:
			f.Type, f.ItemType = "list", "string"
		case ft.Kind() == reflect.Map:
			f.Type = "map"
		case ft.Kind() == reflect.Bool:
			f.Type = "bool"
		default:
			f.Type = "string"
		}

		out = append(out, f)
	}

	return out
}

// tableRow is one row of a README reference table: Default and Effect, the
// third and fourth of its "| Key | Type | Default | Effect |" columns.
type tableRow struct {
	Default string
	Effect  string
}

// tableKeyPattern matches a cell that is exactly a backtick-quoted key, the
// shape the first column takes on a real data row — never on the header row
// ("Key") or the separator row ("---"), which is what tells parseTable which
// lines to skip without a special case for either. Key characters are
// letters, digits, "_", "." and "[]" — loose enough for the project table's
// "sessions[].name" shape, not just the config table's plain dotted keys
// pkg/config/doc_test.go's own regex expects.
var tableKeyPattern = regexp.MustCompile("^`([a-zA-Z0-9_.\\[\\]]+)`$")

// parseTable extracts every row's Default (3rd column) and Effect (4th
// column), keyed by the 1st column's key — mirroring
// pkg/config/doc_test.go's keysInTable, extended to two columns instead of
// one.
//
// Splits by hand, line by line, rather than one cross-row regex: a handful
// of Effect cells contain a markdown-escaped "\|" (the `language`/
// `restore_layout` rows show `fr` \| `en` inside a *Type* cell, to list
// their enum), and naively splitting on every "|" would misread that as a
// column boundary and shift every column after it. "\|" is swapped for a
// placeholder before splitting and restored after, on each column.
func parseTable(text string) map[string]tableRow {
	rows := make(map[string]tableRow)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}

		escaped := strings.ReplaceAll(line, "\\|", "\x00")

		cols := strings.Split(strings.Trim(escaped, "|"), "|")
		if len(cols) < 4 {
			continue
		}

		for i := range cols {
			cols[i] = strings.ReplaceAll(strings.TrimSpace(cols[i]), "\x00", "|")
		}

		m := tableKeyPattern.FindStringSubmatch(cols[0])
		if m == nil {
			continue
		}

		rows[m[1]] = tableRow{Default: cols[2], Effect: strings.Join(cols[3:], "|")}
	}

	return rows
}

// section returns the README text between header and the next heading —
// copied from pkg/config/doc_test.go's own section helper (unexported there,
// and not worth promoting out of a _test.go file for one other caller).
func section(readme, header string) string {
	_, after, found := strings.Cut(readme, header)
	if !found {
		return ""
	}

	if end := strings.Index(after, "\n## "); end >= 0 {
		after = after[:end]
	}

	if end := strings.Index(after, "\n### "); end >= 0 {
		after = after[:end]
	}

	return after
}
