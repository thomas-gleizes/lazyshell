// Package config loads lazyshell's user configuration: a YAML file merged
// onto hardcoded defaults, so a missing or partial file is never an error.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// defaultScrollbackSize mirrors vt.DefaultScrollbackSize (pkg/screen's
// terminal emulator dependency): duplicated rather than imported so this
// package stays free of any gocui/vt dependency.
const defaultScrollbackSize = 10000

// defaultSessionsPanelWidth is today's hardcoded sessions panel width
// (pkg/gui/layout.go's sessionsWidthLandscape).
const defaultSessionsPanelWidth = 30

// defaultPrefixKey is the tmux-style pass-through escape prefix, in
// gocui.Parse syntax (pkg/gui/input.go's defaultPrefixKey).
const defaultPrefixKey = "Ctrl+B"

// Config is lazyshell's user-facing configuration. Every field has a
// meaningful default (see Default), so a config file only needs to mention
// the fields it wants to override.
type Config struct {
	// Shell is the command started behind each new session's pty. Empty means
	// "use $SHELL, falling back to /bin/bash" (resolved at use, not at load,
	// so Default() does not need to touch the environment).
	Shell string `yaml:"shell"`
	// ScrollbackSize is the maximum number of lines a session's terminal
	// emulator keeps once they scroll off-screen.
	ScrollbackSize int `yaml:"scrollback_size"`
	// SessionsPanelWidth is the sessions list's width in landscape mode
	// (columns), or height in portrait mode (rows) — see pkg/gui/layout.go.
	SessionsPanelWidth int `yaml:"sessions_panel_width"`
	// PrefixKey is the pass-through escape prefix, in gocui.Parse syntax
	// ("Ctrl+A", "Ctrl+Space", ...). Overridable at runtime via
	// $LAZYSHELL_PREFIX, which wins over this value.
	PrefixKey string `yaml:"prefix_key"`
	// Keybindings remaps actions (stable ids such as "new_session") to a
	// gocui.Parse key spec. An action missing from this map keeps its
	// built-in default.
	Keybindings map[string]string `yaml:"keybindings"`
	// Theme overrides the UI's colors. An empty field keeps its built-in
	// default (see pkg/gui's Theme/defaultTheme).
	Theme Theme `yaml:"theme"`
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

// Default returns the configuration lazyshell runs with when there is no
// config file, and what a partial file is merged onto.
func Default() Config {
	return Config{
		Shell:              "",
		ScrollbackSize:     defaultScrollbackSize,
		SessionsPanelWidth: defaultSessionsPanelWidth,
		PrefixKey:          defaultPrefixKey,
		Keybindings:        map[string]string{},
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

	return cfg, nil
}
