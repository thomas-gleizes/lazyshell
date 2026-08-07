package app

import (
	"errors"
	"flag"
	"testing"
)

func TestParseArgsDefaults(t *testing.T) {
	inv, err := ParseArgs(nil)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}

	if inv.Command != CommandRun || inv.ConfigFile != "" || inv.NoAutostart {
		t.Errorf("ParseArgs(nil) = %+v, want the plain run invocation", inv)
	}
}

func TestParseArgsConfigFileForms(t *testing.T) {
	for _, args := range [][]string{
		{"--config-file=/p/x.yml"},
		{"--config-file", "/p/x.yml"},
		{"-config-file=/p/x.yml"},
		{"-f", "/p/x.yml"},
		{"-f=/p/x.yml"},
	} {
		inv, err := ParseArgs(args)
		if err != nil {
			t.Errorf("ParseArgs(%v): %v", args, err)

			continue
		}

		if inv.ConfigFile != "/p/x.yml" {
			t.Errorf("ParseArgs(%v).ConfigFile = %q, want /p/x.yml", args, inv.ConfigFile)
		}
	}
}

func TestParseArgsNoAutostart(t *testing.T) {
	inv, err := ParseArgs([]string{"--no-autostart"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}

	if !inv.NoAutostart {
		t.Error("NoAutostart = false, want true")
	}
}

func TestParseArgsSubCommands(t *testing.T) {
	inv, err := ParseArgs([]string{"init"})
	if err != nil {
		t.Fatalf("ParseArgs init: %v", err)
	}
	if inv.Command != CommandInit {
		t.Errorf("Command = %q, want init", inv.Command)
	}

	inv, err = ParseArgs([]string{"allow", "./lazyshell.yml"})
	if err != nil {
		t.Fatalf("ParseArgs allow: %v", err)
	}
	if inv.Command != CommandAllow || inv.Arg != "./lazyshell.yml" {
		t.Errorf("ParseArgs allow = %+v, want the allow command with its path", inv)
	}

	inv, err = ParseArgs([]string{"allow"})
	if err != nil {
		t.Fatalf("ParseArgs bare allow: %v", err)
	}
	if inv.Command != CommandAllow || inv.Arg != "" {
		t.Errorf("ParseArgs bare allow = %+v, want the allow command with no path", inv)
	}
}

func TestParseArgsRejectsGarbage(t *testing.T) {
	for _, args := range [][]string{
		{"nope"},
		{"--unknown-flag"},
		{"init", "extra"},
		{"stray-positional"},
		{"hook"},
	} {
		if _, err := ParseArgs(args); err == nil {
			t.Errorf("ParseArgs(%v): want error, got nil", args)
		}
	}
}

func TestParseArgsHook(t *testing.T) {
	inv, err := ParseArgs([]string{"hook", "working"})
	if err != nil {
		t.Fatalf("ParseArgs hook: %v", err)
	}
	if inv.Command != CommandHook || inv.Arg != "working" {
		t.Errorf("ParseArgs hook working = %+v, want the hook command with its event", inv)
	}
}

func TestParseArgsInitAgents(t *testing.T) {
	inv, err := ParseArgs([]string{"init", "--agents"})
	if err != nil {
		t.Fatalf("ParseArgs init --agents: %v", err)
	}
	if inv.Command != CommandInit || !inv.Agents {
		t.Errorf("ParseArgs init --agents = %+v, want CommandInit with Agents=true", inv)
	}
}

func TestParseArgsHelpIsNotAnError(t *testing.T) {
	// -h must be recognisable as a help request rather than a usage mistake, so
	// Main can print the usage and exit 0.
	_, err := ParseArgs([]string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("ParseArgs(-h) error = %v, want flag.ErrHelp", err)
	}
}
