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
	defaultSessionsPanelWidth = 40
	// defaultSessionsPanelHeight is its height in portrait mode
	// (pkg/gui/layout.go's sessionsHeightPortrait).
	defaultSessionsPanelHeight = 10
	// defaultPortraitMaxWidth/defaultPortraitMinHeight are the thresholds at
	// which the layout stacks the panels instead of splitting them side by side
	// (pkg/gui/layout.go's isPortrait).
	defaultPortraitMaxWidth  = 84
	defaultPortraitMinHeight = 45
	// defaultPrefixKey is the pass-through escape key, in gocui.Parse syntax
	// (pkg/gui/input.go's defaultPrefixKey, which explains why it is not the
	// tmux-style Ctrl+B any more).
	defaultPrefixKey = "Ctrl+O"
	// defaultRefreshIntervalMs is the UI's redraw tick (pkg/gui/gui.go's
	// reRenderInterval), in milliseconds.
	defaultRefreshIntervalMs = 30
	// defaultKillTimeoutMs is how long killing a session waits before escalating
	// to SIGKILL (pkg/session's DefaultKillTimeout), in milliseconds.
	defaultKillTimeoutMs = 2000
	// defaultTerm is the TERM value sessions are started with — a value the
	// bundled emulator actually implements (pkg/session/manager.go's buildEnv).
	defaultTerm = "xterm-256color"
	// defaultBellMarker/defaultAltScreenMarker/defaultActivityMarker/
	// defaultBroadcastMarker are the sessions list's gutter markers
	// (pkg/gui/sessions_panel.go).
	defaultBellMarker      = "!"
	defaultAltScreenMarker = "#"
	defaultActivityMarker  = "●"
	defaultBroadcastMarker = "+"
	// defaultAgentIdleMarker/defaultAgentWorkingMarker/defaultAgentBlockedMarker/
	// defaultAgentDoneMarker are the gutter markers for an AI agent session's
	// detected state (pkg/agent, pkg/gui/sessions_panel.go). Distinct from
	// activityMarker on purpose: activity only means "produced output", these
	// mean "is it waiting on you".
	defaultAgentIdleMarker    = "·"
	defaultAgentWorkingMarker = "…"
	defaultAgentBlockedMarker = "‼"
	defaultAgentDoneMarker    = "✓"
	// defaultHalfPageDivisor is what the output panel's height is divided by for
	// a Ctrl-U/Ctrl-D scroll (pkg/gui/input.go's scrollHalfPage).
	defaultHalfPageDivisor = 2
	// defaultMouseWheelLines is how many lines one wheel notch scrolls the
	// output panel by (pkg/gui/mouse.go). Three is what terminals themselves
	// use for their own scrollback.
	defaultMouseWheelLines = 3
	// defaultPerfRefreshIntervalMs is how often every session's processes are
	// sampled for the resources tab (pkg/gui/perf_sampler.go). Five seconds:
	// this runs in the background whether or not that tab is open, a CPU
	// percentage needs a wide enough window to mean anything, and on macOS
	// each sample costs a `ps` spawn.
	defaultPerfRefreshIntervalMs = 5000
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
	// PrefixKey is the pass-through escape key, in gocui.Parse syntax
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
	// Notify configures the desktop notification sent when a detected AI
	// agent session goes blocked or done (pkg/agent).
	Notify Notify `yaml:"notify"`
	// WindowTitle configures whether the host terminal's window/tab title
	// tracks the focused session's name and live OSC title.
	WindowTitle WindowTitle `yaml:"window_title"`
	// Mouse configures clicking, wheel scrolling and drag selection.
	Mouse Mouse `yaml:"mouse"`
	// EnvTab configures the output panel's env tab.
	EnvTab EnvTab `yaml:"env_tab"`
	// Perf configures the output panel's perf tab.
	Perf Perf `yaml:"perf"`
	// AgentStatsCommand, when set, is run for the selected session — with
	// $LAZYSHELL_SESSION_ID in its environment — and its first line of
	// stdout is shown alongside the turn duration. Best-effort: lazyshell
	// does not parse token/cost data itself, the same "external command
	// whose output line is displayed" shape as Claude Code's own
	// statusLine. Empty means no stats line.
	AgentStatsCommand string `yaml:"agent_stats_command"`

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
	TabActiveColor         string `yaml:"tab_active_color"`
}

// Markers is the four-column gutter every session line starts with — the
// only way to learn something about a session that is not the one on screen.
type Markers struct {
	// Bell flags a session that emitted a BEL since it was last looked at.
	Bell string `yaml:"bell"`
	// AltScreen flags a session with a full-screen application in control.
	AltScreen string `yaml:"alt_screen"`
	// Activity flags a session that produced output since it was last looked
	// at, other than the one currently selected.
	Activity string `yaml:"activity"`
	// Broadcast flags a session marked to receive broadcast keystrokes.
	Broadcast string `yaml:"broadcast"`
	// AgentIdle/AgentWorking/AgentBlocked/AgentDone flag a detected AI agent
	// session's state (pkg/agent) — idle, working, waiting on you, or done
	// with its turn. Empty for a session that is not running a known agent.
	AgentIdle    string `yaml:"agent_idle"`
	AgentWorking string `yaml:"agent_working"`
	AgentBlocked string `yaml:"agent_blocked"`
	AgentDone    string `yaml:"agent_done"`
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

// Notify configures the notification sent when a detected AI agent session
// goes blocked or done. Same shape and the same reasoning as Clipboard: OSC
// (9 and 777 here, both sent unconditionally — an unsupported one is simply
// ignored by the terminal, so sending both costs nothing and reaches more
// terminals than picking one) is a write with no acknowledgement, so the
// fallback is a manual switch rather than something auto-detected: empty
// means OSC only, set means this command runs instead, with the notification
// text on its stdin.
type Notify struct {
	FallbackCommand string `yaml:"fallback_command"`
}

// WindowTitle configures the OSC 0 sequence lazyshell writes to the host
// terminal so its window/tab title follows the focused session: its name,
// plus whatever OSC 0/2 title the program running inside that session's pty
// last set (pkg/screen's Screen.Title), when it has set one. Unlike
// Clipboard/Notify there is no fallback command — an unsupported OSC 0 is
// simply ignored by the terminal, so the only knob needed is on/off.
type WindowTitle struct {
	Enabled bool `yaml:"enabled"`
}

// EnvTab configures the output panel's env tab, which lists the environment a
// session's shell was launched with.
type EnvTab struct {
	// MaskSecrets replaces the value of any variable whose *name* looks like a
	// credential (TOKEN, SECRET, PASSWORD, AUTH, ..._KEY) with a fixed-width
	// mask. On by default: the panel is as shareable as a screenshot of it,
	// and an API key must not be what makes a screenshot dangerous. Set to
	// false to see the real values.
	MaskSecrets bool `yaml:"mask_secrets"`
}

// Perf configures the output panel's perf tab, which reports what a session's
// process is consuming.
type Perf struct {
	// RefreshIntervalMs is how often the tab actually samples the process,
	// independently of RefreshIntervalMs's redraw tick. Sampling is the one
	// genuinely expensive thing this panel does — on macOS it spawns a `ps`,
	// for want of a cgo-free alternative — so it is deliberately an order of
	// magnitude slower than the redraw, and a CPU percentage is meaningless
	// over a 30 ms window anyway.
	RefreshIntervalMs int `yaml:"refresh_interval_ms"`
}

// Mouse configures the mouse support. It is on by default, and the switch
// exists because enabling it is not free: gocui gives mouse buttons and the
// Shift-Up/Shift-Down keys the very same values (MouseLeft is KeyShiftArrowDown,
// MouseRight is KeyShiftArrowUp), so the two cannot be told apart. lazyshell
// resolves the ambiguity in favour of the mouse and stops forwarding those two
// keys to the session — see docs/adr/0003-souris.md. Setting Enabled to false
// gives them back.
type Mouse struct {
	// Enabled turns the whole feature on or off, including gocui's own mouse
	// reporting, which hands wheel and selection back to the host terminal.
	Enabled bool `yaml:"enabled"`
	// WheelLines is how many lines one wheel notch scrolls the output panel
	// by. Zero falls back to the built-in default.
	WheelLines int `yaml:"wheel_lines"`
	// ForwardToApp lets a full-screen program running inside a session (vim
	// with `set mouse=a`, htop) receive the mouse events itself, but only once
	// it has explicitly asked for them with a DECSET 9/1000/1002/1003. A
	// program that never asks — a shell, an AI agent CLI — never sees them,
	// and the wheel keeps scrolling lazyshell's own scrollback.
	ForwardToApp bool `yaml:"forward_to_app"`
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
			Bell:         defaultBellMarker,
			AltScreen:    defaultAltScreenMarker,
			Activity:     defaultActivityMarker,
			Broadcast:    defaultBroadcastMarker,
			AgentIdle:    defaultAgentIdleMarker,
			AgentWorking: defaultAgentWorkingMarker,
			AgentBlocked: defaultAgentBlockedMarker,
			AgentDone:    defaultAgentDoneMarker,
		},
		Scroll: Scroll{
			PageLines:       0,
			HalfPageDivisor: defaultHalfPageDivisor,
		},
		Clipboard:   Clipboard{FallbackCommand: ""},
		Notify:      Notify{FallbackCommand: ""},
		WindowTitle: WindowTitle{Enabled: true},
		Mouse: Mouse{
			Enabled:      true,
			WheelLines:   defaultMouseWheelLines,
			ForwardToApp: true,
		},
		EnvTab:            EnvTab{MaskSecrets: true},
		Perf:              Perf{RefreshIntervalMs: defaultPerfRefreshIntervalMs},
		AgentStatsCommand: "",
	}
}

// Path resolves the config file's location: $LAZYSHELL_CONFIG if set (mainly
// so tests never touch the real home directory), else
// $XDG_CONFIG_HOME/lazyshell/config.yml, else ~/.config/lazyshell/config.yml.
func Path() string {
	if p := os.Getenv("LAZYSHELL_CONFIG"); p != "" {
		return p
	}

	dir := configDir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "config.yml")
}

// AgentsDir resolves the directory pkg/agent scans for user-supplied AI
// agent detection manifests that override or extend the built-in ones —
// $XDG_CONFIG_HOME/lazyshell/agents, else ~/.config/lazyshell/agents. Shares
// Path's XDG resolution rather than $LAZYSHELL_CONFIG, which names a single
// file, not a directory to derive one from.
func AgentsDir() string {
	dir := configDir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "agents")
}

// DebugLogPath resolves the file --debug appends to:
// $XDG_CONFIG_HOME/lazyshell/debug.log, else ~/.config/lazyshell/debug.log —
// next to config.yml, so there is a single lazyshell directory to know about.
// Shares AgentsDir's reasoning for ignoring $LAZYSHELL_CONFIG: that variable
// names one file, not a directory to derive others from. Empty when the home
// directory cannot be determined, which pkg/debug reports as an error.
func DebugLogPath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "debug.log")
}

// configDir is lazyshell's config directory, shared by Path and AgentsDir:
// $XDG_CONFIG_HOME/lazyshell, else ~/.config/lazyshell.
func configDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}

		base = filepath.Join(home, ".config")
	}

	return filepath.Join(base, "lazyshell")
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
