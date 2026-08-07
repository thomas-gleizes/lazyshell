// Package config loads lazyshell's user configuration: a YAML file merged
// onto hardcoded defaults, so a missing or partial file is never an error.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// The built-in defaults. Each one mirrors a constant that used to be hardcoded
// in the package that consumes it; the constant stays there as that package's
// own fallback, and these are what the config layer hands it instead.
const (
	// defaultLanguage is the language the UI is written in today. Reserved and
	// validated now, applied by the i18n phase — see Config.Language.
	defaultLanguage = "fr"
	// defaultScrollbackSize mirrors vt.DefaultScrollbackSize (pkg/screen's
	// terminal emulator dependency): duplicated rather than imported so this
	// package stays free of any gocui/vt dependency.
	defaultScrollbackSize = 10000
	// defaultSessionsPanelWidth is the sessions panel's width in landscape mode
	// (pkg/gui/layout.go's sessionsWidthLandscape).
	defaultSessionsPanelWidth = 30
	// defaultSessionsPanelHeight is its height in portrait mode
	// (pkg/gui/layout.go's sessionsHeightPortrait).
	defaultSessionsPanelHeight = 10
	// defaultPortraitMaxWidth/defaultPortraitMinHeight are the thresholds at
	// which the layout stacks the panels instead of splitting them side by side
	// (pkg/gui/layout.go's isPortrait).
	defaultPortraitMaxWidth  = 84
	defaultPortraitMinHeight = 45
	// defaultPrefixKey is the tmux-style pass-through escape prefix, in
	// gocui.Parse syntax (pkg/gui/input.go's defaultPrefixKey).
	defaultPrefixKey = "Ctrl+B"
	// defaultRefreshIntervalMs is the UI's redraw tick (pkg/gui/gui.go's
	// reRenderInterval), in milliseconds.
	defaultRefreshIntervalMs = 30
	// defaultKillTimeoutMs is how long killing a session waits before escalating
	// to SIGKILL (pkg/session's DefaultKillTimeout), in milliseconds.
	defaultKillTimeoutMs = 2000
	// defaultTerm is the TERM value sessions are started with — a value the
	// bundled emulator actually implements (pkg/session/manager.go's buildEnv).
	defaultTerm = "xterm-256color"
	// defaultBellMarker/defaultAltScreenMarker/defaultActivityMarker are the
	// sessions list's gutter markers (pkg/gui/sessions_panel.go).
	defaultBellMarker      = "!"
	defaultAltScreenMarker = "#"
	defaultActivityMarker  = "●"
	// defaultHalfPageDivisor is what the output panel's height is divided by for
	// a Ctrl-U/Ctrl-D scroll (pkg/gui/input.go's scrollHalfPage).
	defaultHalfPageDivisor = 2
)

// Config is lazyshell's user-facing configuration. Every field has a
// meaningful default (see Default), so a config file only needs to mention
// the fields it wants to override.
//
// Adding a field here is not enough to ship it: it must be wired to whatever
// used to hardcode it, validated in Validate, and documented in the README's
// reference table — doc_test.go fails the build otherwise.
type Config struct {
	// Language is the UI language, "fr" or "en" — see pkg/i18n. Covers the
	// interactive TUI (bindings, popups, status bar, footers, session
	// messages); pkg/app's CLI output (`lazyshell config ...`) is unaffected
	// and stays French, since it can run before a config file — and so a
	// Language — has even been loaded.
	Language string `yaml:"language"`
	// Shell is the command started behind each new session's pty. Empty means
	// "use $SHELL, falling back to /bin/bash" (resolved at use, not at load,
	// so Default() does not need to touch the environment).
	Shell string `yaml:"shell"`
	// Term is the TERM value every session is started with. Lowering it below
	// the bundled emulator's actual capabilities is the point of exposing it:
	// some programs behave better when told less.
	Term string `yaml:"term"`
	// ScrollbackSize is the maximum number of lines a session's terminal
	// emulator keeps once they scroll off-screen.
	ScrollbackSize int `yaml:"scrollback_size"`
	// SessionsPanelWidth is the sessions list's width in landscape mode, in
	// columns — see pkg/gui/layout.go.
	SessionsPanelWidth int `yaml:"sessions_panel_width"`
	// SessionsPanelHeight is the sessions list's height in portrait mode, in
	// rows.
	SessionsPanelHeight int `yaml:"sessions_panel_height"`
	// PortraitMaxWidth and PortraitMinHeight are the terminal geometry at which
	// the layout switches to stacking the panels: portrait applies when the
	// terminal is at most PortraitMaxWidth columns wide *and* more than
	// PortraitMinHeight rows tall.
	PortraitMaxWidth  int `yaml:"portrait_max_width"`
	PortraitMinHeight int `yaml:"portrait_min_height"`
	// RefreshIntervalMs is how often, in milliseconds, the sessions list and
	// the output panel are re-rendered. Lower is smoother and costs more CPU;
	// an unchanged panel is never pushed, so idle cost stays near zero either
	// way.
	RefreshIntervalMs int `yaml:"refresh_interval_ms"`
	// KillTimeoutMs is how long, in milliseconds, killing a session waits after
	// SIGTERM before escalating to SIGKILL, and again after that before giving
	// up.
	KillTimeoutMs int `yaml:"kill_timeout_ms"`
	// PrefixKey is the pass-through escape prefix, in gocui.Parse syntax
	// ("Ctrl+A", "Ctrl+Space", ...). Overridable at runtime via
	// $LAZYSHELL_PREFIX, which wins over this value.
	PrefixKey string `yaml:"prefix_key"`
	// Keybindings remaps actions (stable ids such as "new_session") to a
	// gocui.Parse key spec. An action missing from this map keeps its
	// built-in default.
	Keybindings map[string]string `yaml:"keybindings"`
	// Markers overrides the sessions list's gutter markers.
	Markers Markers `yaml:"markers"`
	// Scroll overrides the output panel's scrolling steps.
	Scroll Scroll `yaml:"scroll"`
	// Theme overrides the UI's colors. An empty field keeps its built-in
	// default (see pkg/gui's Theme/defaultTheme).
	Theme Theme `yaml:"theme"`
	// Clipboard configures copy-mode's yank.
	Clipboard Clipboard `yaml:"clipboard"`

	// Warnings lists the keys the file contained but this struct has no field
	// for, so that a typo says why it does nothing instead of being silently
	// dropped. Filled by Load; never read from the file itself.
	Warnings []string `yaml:"-"`
}

// Theme is the color part of Config, kept as plain strings (W3C color names
// or "#rrggbb", gocui.GetColor's syntax) so this package stays free of a
// gocui dependency — pkg/gui resolves them to actual gocui Attributes.
type Theme struct {
	ActiveBorderColor      string `yaml:"active_border_color"`
	InactiveBorderColor    string `yaml:"inactive_border_color"`
	SelectedBgColor        string `yaml:"selected_bg_color"`
	PassThroughBorderColor string `yaml:"pass_through_border_color"`
}

// Markers is the two-column gutter every session line starts with — the only
// way to learn something about a session that is not the one on screen.
type Markers struct {
	// Bell flags a session that emitted a BEL since it was last looked at.
	Bell string `yaml:"bell"`
	// AltScreen flags a session with a full-screen application in control.
	AltScreen string `yaml:"alt_screen"`
	// Activity flags a session that produced output since it was last looked
	// at, other than the one currently selected.
	Activity string `yaml:"activity"`
}

// Clipboard configures how copy-mode's yank leaves lazyshell. There is no
// reliable way to detect whether the host terminal actually accepted an OSC
// 52 sequence, so this is a manual switch rather than a fallback the code
// decides on its own: empty means "OSC 52 only", the choice that works
// through SSH and needs no binary installed; set means "run this command
// instead, with the yanked text on its stdin" — for a terminal that does not
// support OSC 52.
type Clipboard struct {
	FallbackCommand string `yaml:"fallback_command"`
}

// Scroll is how far the output panel moves per scrolling keystroke.
type Scroll struct {
	// PageLines is how many lines PgUp/PgDn move by. Zero means "one full
	// panel height", which is what a page key normally does.
	PageLines int `yaml:"page_lines"`
	// HalfPageDivisor is what the panel height is divided by for Ctrl-U and
	// Ctrl-D. The default of 2 is the half page the keys are named after; 4
	// gives a quarter page.
	HalfPageDivisor int `yaml:"half_page_divisor"`
}

// Default returns the configuration lazyshell runs with when there is no
// config file, and what a partial file is merged onto.
func Default() Config {
	return Config{
		Language:            defaultLanguage,
		Shell:               "",
		Term:                defaultTerm,
		ScrollbackSize:      defaultScrollbackSize,
		SessionsPanelWidth:  defaultSessionsPanelWidth,
		SessionsPanelHeight: defaultSessionsPanelHeight,
		PortraitMaxWidth:    defaultPortraitMaxWidth,
		PortraitMinHeight:   defaultPortraitMinHeight,
		RefreshIntervalMs:   defaultRefreshIntervalMs,
		KillTimeoutMs:       defaultKillTimeoutMs,
		PrefixKey:           defaultPrefixKey,
		Keybindings:         map[string]string{},
		Markers: Markers{
			Bell:      defaultBellMarker,
			AltScreen: defaultAltScreenMarker,
			Activity:  defaultActivityMarker,
		},
		Scroll: Scroll{
			PageLines:       0,
			HalfPageDivisor: defaultHalfPageDivisor,
		},
		Clipboard: Clipboard{FallbackCommand: ""},
	}
}

// Path resolves the config file's location: $LAZYSHELL_CONFIG if set (mainly
// so tests never touch the real home directory), else
// $XDG_CONFIG_HOME/lazyshell/config.yml, else ~/.config/lazyshell/config.yml.
func Path() string {
	if p := os.Getenv("LAZYSHELL_CONFIG"); p != "" {
		return p
	}

	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}

		base = filepath.Join(home, ".config")
	}

	return filepath.Join(base, "lazyshell", "config.yml")
}

// Load reads the YAML file at path and merges it onto Default(). A missing
// file is not an error — it just means "run with the defaults". Fields
// absent from the file are left untouched by yaml.Unmarshal, which is what
// makes the merge work: Unmarshal only sets the keys it actually finds.
//
// Keys the file contains but Config has no field for end up in Warnings rather
// than being dropped in silence: `session_panel_width` (one letter short) is
// otherwise indistinguishable, from the user's side, from lazyshell ignoring
// the config file entirely.
func Load(path string) (Config, error) {
	cfg := Default()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}

		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg.Warnings = unknownKeys(data, &Config{})

	return cfg, nil
}
