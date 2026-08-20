package config

import "fmt"

// NumericBound is one entry of NumericBounds — see its doc comment.
type NumericBound struct {
	// Key is the field's dotted config key, e.g. "scroll.half_page_divisor".
	Key string
	// Min and Max bound the field. Max == 0 means "no upper bound" — the same
	// convention clamp itself uses.
	Min, Max int
}

// NumericBounds is every int field Validate clamps, keyed by dotted config
// key, exported so the config-schema generator (cmd/gen-config-schema) can
// advertise the exact same bounds a running lazyshell would enforce, instead
// of a second hand-copied table that could quietly drift from this one.
//
// Bounds are deliberately loose: the point is to catch a value that would
// break the UI (a zero-width panel, a redraw loop that pins a core), not to
// second-guess someone who wants a 400-column sessions list on an ultrawide.
var NumericBounds = []NumericBound{
	{"refresh_interval_ms", 10, 1000},
	{"kill_timeout_ms", 100, 0},
	{"scrollback_size", 0, 0},
	{"sessions_panel_width", 5, 0},
	{"sessions_panel_height", 5, 0},
	{"agents_panel_height", 3, 0},
	{"portrait_max_width", 0, 0},
	{"portrait_min_height", 0, 0},
	{"scroll.page_lines", 0, 0},
	{"scroll.half_page_divisor", 1, 0},
	{"mouse.wheel_lines", 1, 0},
}

// numericField resolves a NumericBounds key to the *int it names on c. Only
// Validate and Default (via each other's *Config) call this, immediately
// after building the key from the same literal NumericBounds slice, so an
// unmatched key would be an immediate nil-pointer panic in Validate's own
// test coverage — not a silent drift.
func (c *Config) numericField(key string) *int {
	switch key {
	case "refresh_interval_ms":
		return &c.RefreshIntervalMs
	case "kill_timeout_ms":
		return &c.KillTimeoutMs
	case "scrollback_size":
		return &c.ScrollbackSize
	case "sessions_panel_width":
		return &c.SessionsPanelWidth
	case "sessions_panel_height":
		return &c.SessionsPanelHeight
	case "agents_panel_height":
		return &c.AgentsPanelHeight
	case "portrait_max_width":
		return &c.PortraitMaxWidth
	case "portrait_min_height":
		return &c.PortraitMinHeight
	case "scroll.page_lines":
		return &c.Scroll.PageLines
	case "scroll.half_page_divisor":
		return &c.Scroll.HalfPageDivisor
	case "mouse.wheel_lines":
		return &c.Mouse.WheelLines
	default:
		return nil
	}
}

// Validate checks every numeric and enumerated field, replacing whatever is out
// of range with its built-in default and reporting what it did.
//
// Same contract as ProjectConfig.Validate: a bad value never stops lazyshell
// from starting. A config file is hand-edited and read once at boot, and the
// terminal is about to be taken over — refusing to start over `refresh_interval_ms: 0`
// would leave the user with an unusable tool and no way to see why. Correcting
// the value and saying so out loud is the only behaviour that is both safe and
// honest.
//
// Key specs (PrefixKey, Keybindings) and colors are *not* checked here: parsing
// them needs gocui, and this package deliberately has no such dependency. They
// are validated by pkg/gui's ValidateKeys and ValidateTheme, called from the
// same place in pkg/app.
func (c *Config) Validate() []error {
	def := Default()

	var errs []error

	// NumericBounds is the single source for both what gets clamped here and
	// what the config-schema generator advertises as a field's min/max — the
	// two must never say something different about the same key. numericField
	// resolves a bound's dotted key to the *int it actually clamps, on this
	// instance and on def, so the bound itself never has to duplicate a field
	// pointer.
	for _, b := range NumericBounds {
		field := c.numericField(b.Key)
		fallback := def.numericField(b.Key)

		if err := clamp(b.Key, field, b.Min, b.Max, *fallback); err != nil {
			errs = append(errs, err)
		}
	}

	// Not in the table above because 0 is a meaningful value here, not an out
	// of range one: it turns background sampling off entirely. Everything
	// between 1 and 100 ms is what gets corrected — sampling spawns a `ps` on
	// macOS, so at that rate it would cost more than what it measures.
	if c.Perf.RefreshIntervalMs < 0 || (c.Perf.RefreshIntervalMs > 0 && c.Perf.RefreshIntervalMs < 100) {
		errs = append(errs, fmt.Errorf(
			"perf.refresh_interval_ms = %d hors bornes (0 pour désactiver, sinon minimum 100), retour à %d",
			c.Perf.RefreshIntervalMs, def.Perf.RefreshIntervalMs))
		c.Perf.RefreshIntervalMs = def.Perf.RefreshIntervalMs
	}

	if !Languages[c.Language] {
		errs = append(errs, fmt.Errorf("language %q inconnue (attendu : %s), retour à %q",
			c.Language, languageList(), def.Language))
		c.Language = def.Language
	}

	switch c.RestoreLayout {
	case RestoreLayoutAsk, RestoreLayoutAlways, RestoreLayoutNever:
	default:
		errs = append(errs, fmt.Errorf(
			"restore_layout %q inconnu (attendu : %s, %s, %s), retour à %q",
			c.RestoreLayout, RestoreLayoutAsk, RestoreLayoutAlways, RestoreLayoutNever, def.RestoreLayout))
		c.RestoreLayout = def.RestoreLayout
	}

	// A marker is drawn in a four-column gutter, one column per marker, and all
	// four can show at once — anything wider would shift every session line and
	// break the fixed-width columns sessionsPanelContent lays out.
	if err := singleColumn("markers.bell", &c.Markers.Bell, def.Markers.Bell); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.alt_screen", &c.Markers.AltScreen, def.Markers.AltScreen); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.activity", &c.Markers.Activity, def.Markers.Activity); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.broadcast", &c.Markers.Broadcast, def.Markers.Broadcast); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.agent_idle", &c.Markers.AgentIdle, def.Markers.AgentIdle); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.agent_working", &c.Markers.AgentWorking, def.Markers.AgentWorking); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.agent_blocked", &c.Markers.AgentBlocked, def.Markers.AgentBlocked); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.agent_done", &c.Markers.AgentDone, def.Markers.AgentDone); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.command_failed", &c.Markers.CommandFailed, def.Markers.CommandFailed); err != nil {
		errs = append(errs, err)
	}

	if err := singleColumn("markers.restart", &c.Markers.Restart, def.Markers.Restart); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// clamp resets *field to fallback when it falls outside [min, max], and says
// so. A max of 0 means "no upper bound".
func clamp(name string, field *int, minimum, maximum, fallback int) error {
	if *field >= minimum && (maximum == 0 || *field <= maximum) {
		return nil
	}

	bad := *field
	*field = fallback

	if maximum == 0 {
		return fmt.Errorf("%s = %d hors bornes (minimum %d), retour à %d", name, bad, minimum, fallback)
	}

	return fmt.Errorf("%s = %d hors bornes (%d à %d), retour à %d", name, bad, minimum, maximum, fallback)
}

// singleColumn keeps a gutter marker to at most one rune. Empty is allowed and
// meaningful: it turns that marker off.
func singleColumn(name string, field *string, fallback string) error {
	if len([]rune(*field)) <= 1 {
		return nil
	}

	bad := *field
	*field = fallback

	return fmt.Errorf("%s = %q fait plus d'une colonne, retour à %q", name, bad, fallback)
}
