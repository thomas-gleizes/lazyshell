package gui

import (
	"strings"
	"testing"
)

func TestEnvTabMasksSecretsByName(t *testing.T) {
	tests := []struct {
		name   string
		entry  string
		masked bool
	}{
		{name: "api token", entry: "GITHUB_TOKEN=ghp_realvalue", masked: true},
		{name: "lowercase", entry: "aws_secret_access_key=abc123", masked: true},
		{name: "password", entry: "DB_PASSWORD=hunter2", masked: true},
		{name: "passwd", entry: "PASSWD=hunter2", masked: true},
		{name: "credential", entry: "GOOGLE_CREDENTIALS=blob", masked: true},
		{name: "auth header", entry: "AUTHORIZATION=Bearer x", masked: true},
		{name: "key suffix", entry: "ANTHROPIC_API_KEY=sk-ant-real", masked: true},
		{name: "ssh key", entry: "SSH_KEY=-----BEGIN", masked: true},

		// The heuristic matches names, never values — these must come through
		// untouched, or the panel stops being useful for the thing people
		// actually open it for.
		{name: "path", entry: "PATH=/usr/bin:/bin", masked: false},
		{name: "home", entry: "HOME=/Users/someone", masked: false},
		{name: "term", entry: "TERM=xterm-256color", masked: false},
		{name: "keyboard, not a key", entry: "KEYBOARD_LAYOUT=fr", masked: false},
		{name: "session id", entry: "LAZYSHELL_SESSION_ID=session-1", masked: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gui, _ := newHeadlessGui(t)

			got := gui.envTabContent([]string{tc.entry})

			_, value, _ := strings.Cut(tc.entry, "=")

			if tc.masked {
				if strings.Contains(got, value) {
					t.Errorf("value %q is visible in:\n%s", value, got)
				}

				if !strings.Contains(got, maskedValue) {
					t.Errorf("no mask in:\n%s", got)
				}

				return
			}

			if !strings.Contains(got, value) {
				t.Errorf("value %q is missing from:\n%s", value, got)
			}
		})
	}
}

// Turning the mask off is the whole point of the option: the panel then has to
// show what is really there.
func TestEnvTabWithoutMaskingShowsRealValues(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.maskSecrets = false

	got := gui.envTabContent([]string{"GITHUB_TOKEN=ghp_realvalue"})

	if !strings.Contains(got, "ghp_realvalue") {
		t.Errorf("the real value is hidden with masking off:\n%s", got)
	}
}

// os.Environ's order is the inherited environment's arbitrary one; the panel
// has to be readable, which means sorted.
func TestEnvTabIsSorted(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	got := gui.envTabContent([]string{"ZED=3", "ALPHA=1", "MIKE=2"})

	alpha := strings.Index(got, "ALPHA=")
	mike := strings.Index(got, "MIKE=")
	zed := strings.Index(got, "ZED=")

	if alpha >= mike || mike >= zed {
		t.Errorf("entries are not sorted:\n%s", got)
	}
}

func TestEnvTabCountsVariables(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	got := gui.envTabContent([]string{"A=1", "B=2", "TOKEN=3"})

	if !strings.Contains(got, "3 variable") {
		t.Errorf("the count is missing from the header:\n%s", got)
	}

	if !strings.Contains(got, "1 masquée") {
		t.Errorf("the masked count is missing from the header:\n%s", got)
	}
}

func TestEnvTabHandlesAnEmptyEnvironment(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if got := gui.envTabContent(nil); got == "" {
		t.Error("envTabContent(nil) is empty, want a message saying so")
	}
}

// A variable with no "=" is not something a shell can produce, but os.Environ
// makes no promise about it either — dropping it silently would be worse than
// showing it.
func TestEnvTabKeepsAMalformedEntry(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	if got := gui.envTabContent([]string{"NOEQUALS"}); !strings.Contains(got, "NOEQUALS") {
		t.Errorf("a nameless entry was dropped:\n%s", got)
	}
}
