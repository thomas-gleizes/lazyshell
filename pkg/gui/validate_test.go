package gui

import (
	"strings"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// Each case is something the application already tolerated by falling back to a
// default — silently. The fallback is the right behaviour; the silence was the
// bug, and these assert it is gone.
func TestValidateConfigReportsWhatWasSilentlyIgnored(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{
			name: "touche illisible",
			cfg:  config.Config{Keybindings: map[string]string{"new_session": "Ctrl+Nope"}},
			want: "new_session",
		},
		{
			name: "action inexistante",
			cfg:  config.Config{Keybindings: map[string]string{"new_sesion": "n"}},
			want: "new_sesion",
		},
		{
			name: "couleur inconnue",
			cfg:  config.Config{Theme: config.Theme{ActiveBorderColor: "chartreusse"}},
			want: "active_border_color",
		},
		{
			name: "préfixe illisible",
			cfg:  config.Config{PrefixKey: "Ctrl+"},
			want: "prefix_key",
		},
		{
			// Parseable, but a printable key can never leave pass-through: every
			// printable key goes to the shell, so this would trap the user.
			name: "préfixe imprimable",
			cfg:  config.Config{PrefixKey: "a"},
			want: "prefix_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateConfig(tt.cfg)

			if len(errs) == 0 {
				t.Fatalf("ValidateConfig(%+v) said nothing, want a report", tt.cfg)
			}

			if !strings.Contains(errs[0].Error(), tt.want) {
				t.Errorf("ValidateConfig() = %v, want a message naming %q", errs, tt.want)
			}
		})
	}
}

// Our own defaults, and an empty config, must both pass: a user who has never
// written a config file must never see a warning about one.
func TestValidateConfigAcceptsDefaults(t *testing.T) {
	for name, cfg := range map[string]config.Config{
		"defaults": config.Default(),
		"empty":    {},
	} {
		if errs := ValidateConfig(cfg); len(errs) > 0 {
			t.Errorf("ValidateConfig(%s) = %v, want no errors", name, errs)
		}
	}
}

// Every action the README tells users they can remap must actually validate,
// bound to its own default key — the round trip a user makes when they copy the
// example and change one line.
func TestValidateConfigAcceptsEveryDocumentedRemap(t *testing.T) {
	cfg := config.Config{Keybindings: readmeExample(t).Keybindings}

	if errs := ValidateConfig(cfg); len(errs) > 0 {
		t.Errorf("ValidateConfig(README keybindings) = %v, want no errors", errs)
	}
}
