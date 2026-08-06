package config

import "fmt"

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

	// Bounds are deliberately loose: the point is to catch a value that would
	// break the UI (a zero-width panel, a redraw loop that pins a core), not to
	// second-guess someone who wants a 400-column sessions list on an ultrawide.
	checks := []struct {
		name     string
		field    *int
		min, max int
		fallback int
	}{
		{"refresh_interval_ms", &c.RefreshIntervalMs, 10, 1000, def.RefreshIntervalMs},
		{"kill_timeout_ms", &c.KillTimeoutMs, 100, 0, def.KillTimeoutMs},
		{"scrollback_size", &c.ScrollbackSize, 0, 0, def.ScrollbackSize},
		{"sessions_panel_width", &c.SessionsPanelWidth, 5, 0, def.SessionsPanelWidth},
		{"sessions_panel_height", &c.SessionsPanelHeight, 5, 0, def.SessionsPanelHeight},
		{"portrait_max_width", &c.PortraitMaxWidth, 0, 0, def.PortraitMaxWidth},
		{"portrait_min_height", &c.PortraitMinHeight, 0, 0, def.PortraitMinHeight},
		{"scroll.page_lines", &c.Scroll.PageLines, 0, 0, def.Scroll.PageLines},
		{"scroll.half_page_divisor", &c.Scroll.HalfPageDivisor, 1, 0, def.Scroll.HalfPageDivisor},
	}

	for _, check := range checks {
		if err := clamp(check.name, check.field, check.min, check.max, check.fallback); err != nil {
			errs = append(errs, err)
		}
	}

	if !Languages[c.Language] {
		errs = append(errs, fmt.Errorf("language %q inconnue (attendu : %s), retour à %q",
			c.Language, languageList(), def.Language))
		c.Language = def.Language
	}

	// A marker is drawn in a three-column gutter, one column per marker, and all
	// three can show at once — anything wider would shift every session line and
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
