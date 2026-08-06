package config

import (
	"os"
	"strings"
	"testing"
)

// The whole point of Validate is that a bad value is corrected and reported,
// never fatal and never silent. Each case below asserts both halves: something
// was said, and the field now holds the default.
func TestValidateCorrectsAndReports(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		check   func(Config) bool
		wantKey string
	}{
		{
			name:    "refresh interval trop court",
			mutate:  func(c *Config) { c.RefreshIntervalMs = 0 },
			check:   func(c Config) bool { return c.RefreshIntervalMs == defaultRefreshIntervalMs },
			wantKey: "refresh_interval_ms",
		},
		{
			name:    "refresh interval trop long",
			mutate:  func(c *Config) { c.RefreshIntervalMs = 5000 },
			check:   func(c Config) bool { return c.RefreshIntervalMs == defaultRefreshIntervalMs },
			wantKey: "refresh_interval_ms",
		},
		{
			name:    "panneau plus étroit qu'utilisable",
			mutate:  func(c *Config) { c.SessionsPanelWidth = 1 },
			check:   func(c Config) bool { return c.SessionsPanelWidth == defaultSessionsPanelWidth },
			wantKey: "sessions_panel_width",
		},
		{
			name:    "scrollback négatif",
			mutate:  func(c *Config) { c.ScrollbackSize = -1 },
			check:   func(c Config) bool { return c.ScrollbackSize == defaultScrollbackSize },
			wantKey: "scrollback_size",
		},
		{
			name:    "kill timeout sous le plancher",
			mutate:  func(c *Config) { c.KillTimeoutMs = 5 },
			check:   func(c Config) bool { return c.KillTimeoutMs == defaultKillTimeoutMs },
			wantKey: "kill_timeout_ms",
		},
		{
			name:    "diviseur de demi-page nul",
			mutate:  func(c *Config) { c.Scroll.HalfPageDivisor = 0 },
			check:   func(c Config) bool { return c.Scroll.HalfPageDivisor == defaultHalfPageDivisor },
			wantKey: "scroll.half_page_divisor",
		},
		{
			name:    "langue inconnue",
			mutate:  func(c *Config) { c.Language = "de" },
			check:   func(c Config) bool { return c.Language == defaultLanguage },
			wantKey: "language",
		},
		{
			name:    "marqueur de plus d'une colonne",
			mutate:  func(c *Config) { c.Markers.Bell = "BEL" },
			check:   func(c Config) bool { return c.Markers.Bell == defaultBellMarker },
			wantKey: "markers.bell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)

			errs := cfg.Validate()

			if !mentions(errs, tt.wantKey) {
				t.Errorf("Validate() = %v, want a message mentioning %q", errs, tt.wantKey)
			}

			if !tt.check(cfg) {
				t.Errorf("Validate() left %s at a rejected value: %+v", tt.wantKey, cfg)
			}
		})
	}
}

// A marker set to the empty string is how you turn it off — it must not be
// "corrected" back to the default, which would make the option impossible to
// use for its main purpose.
func TestValidateAcceptsEmptyMarker(t *testing.T) {
	cfg := Default()
	cfg.Markers.Bell = ""

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Errorf("Validate() = %v, want no complaint about an intentionally disabled marker", errs)
	}

	if cfg.Markers.Bell != "" {
		t.Errorf("Markers.Bell = %q, want it left empty", cfg.Markers.Bell)
	}
}

// A single multi-byte rune is one column: the check is on runes, not bytes,
// or an emoji marker would be rejected for being three characters long.
func TestValidateAcceptsMultiByteMarker(t *testing.T) {
	cfg := Default()
	cfg.Markers.AltScreen = "●"

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Errorf("Validate() = %v, want no complaint about a single multi-byte marker", errs)
	}

	if cfg.Markers.AltScreen != "●" {
		t.Errorf("Markers.AltScreen = %q, want it kept", cfg.Markers.AltScreen)
	}
}

func TestValidateAcceptsTheDefaults(t *testing.T) {
	cfg := Default()

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Errorf("Validate(Default()) = %v, want no errors — our own defaults must pass our own rules", errs)
	}
}

// An unknown key is the most common config mistake and used to be the most
// silent one: it lands nowhere, and nothing says so.
func TestLoadReportsUnknownKeys(t *testing.T) {
	path := t.TempDir() + "/config.yml"

	// One typo at the top level, one inside a nested block, to check the
	// warnings are not limited to the outermost mapping.
	body := "session_panel_width: 50\ntheme:\n  activ_border_color: green\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() = %v, want no error: an unknown key must never stop lazyshell from starting", err)
	}

	if len(cfg.Warnings) < 2 {
		t.Fatalf("Warnings = %v, want one per unknown key", cfg.Warnings)
	}

	for _, want := range []string{"session_panel_width", "activ_border_color"} {
		if !containsAny(cfg.Warnings, want) {
			t.Errorf("Warnings = %v, want one mentioning %q", cfg.Warnings, want)
		}
	}

	// The valid part of the file must still be gone through: warning about a
	// typo is only useful if the rest was applied.
	if cfg.SessionsPanelWidth != defaultSessionsPanelWidth {
		t.Errorf("SessionsPanelWidth = %d, want the default %d — the typo must not have taken effect",
			cfg.SessionsPanelWidth, defaultSessionsPanelWidth)
	}
}

func TestLoadWithoutFileHasNoWarnings(t *testing.T) {
	cfg, err := Load(t.TempDir() + "/absent.yml")
	if err != nil {
		t.Fatalf("Load(missing) = %v, want no error", err)
	}

	if len(cfg.Warnings) > 0 {
		t.Errorf("Warnings = %v, want none for a missing file", cfg.Warnings)
	}
}

func mentions(errs []error, key string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), key) {
			return true
		}
	}

	return false
}

func containsAny(values []string, want string) bool {
	for _, v := range values {
		if strings.Contains(v, want) {
			return true
		}
	}

	return false
}
