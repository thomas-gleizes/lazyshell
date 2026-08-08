package session

import (
	"strings"
	"testing"
)

// lookupEnv returns the last value name takes in env — "last wins" is
// exactly how exec resolves duplicates, so this is what the child actually
// sees, not merely what was appended first.
func lookupEnv(env []string, name string) (string, bool) {
	value := ""
	found := false

	for _, entry := range env {
		if after, ok := strings.CutPrefix(entry, name+"="); ok {
			value = after
			found = true
		}
	}

	return value, found
}

// The absence of $LAZYSHELL_CONTROL_SOCK is how a session learns the control
// API is off (pkg/control, config.Control.Enabled). A variable injected
// anyway — even pointing at nothing — would tell every agent inside every
// session that the feature exists and is worth dialling.
func TestControlSocketIsNotInjectedWhenTheAPIIsOff(t *testing.T) {
	m := NewManager()

	env, err := buildEnv(m, "session-id", "/tmp/session-id.sock", t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	if value, ok := lookupEnv(env, "LAZYSHELL_CONTROL_SOCK"); ok {
		t.Errorf("LAZYSHELL_CONTROL_SOCK = %q with the control API off, want it absent", value)
	}
}

func TestControlSocketIsInjectedWhenTheAPIIsOn(t *testing.T) {
	m := NewManager()
	m.ControlSocket = "/tmp/lazyshell-control.sock"

	env, err := buildEnv(m, "session-id", "/tmp/session-id.sock", t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	if value, _ := lookupEnv(env, "LAZYSHELL_CONTROL_SOCK"); value != m.ControlSocket {
		t.Errorf("LAZYSHELL_CONTROL_SOCK = %q, want %q", value, m.ControlSocket)
	}
}

// The control socket sits in buildEnv's layer 2 with $LAZYSHELL_SESSION_ID
// and $LAZYSHELL_SOCK, and shares their actual precedence: opts.Env, the last
// layer, does override it — a project file's declarative env: can point ctl at
// a socket of its choosing, exactly as it already can for the hook channel.
//
// Pinned here rather than left implicit because it is the kind of thing a
// reader assumes goes the other way. It is not a new hole: a project file is
// trust-gated (pkg/config's trust.go) precisely because approving one already
// means letting it start commands.
func TestOptionsEnvOverridesTheControlSocketLikeTheHookOne(t *testing.T) {
	m := NewManager()
	m.ControlSocket = "/tmp/lazyshell-control.sock"

	env, err := buildEnv(m, "session-id", "/tmp/session-id.sock", t.TempDir(), Options{
		Env: map[string]string{
			"LAZYSHELL_CONTROL_SOCK": "/tmp/autre.sock",
			"LAZYSHELL_SOCK":         "/tmp/autre-hook.sock",
		},
	})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	control, _ := lookupEnv(env, "LAZYSHELL_CONTROL_SOCK")
	hook, _ := lookupEnv(env, "LAZYSHELL_SOCK")

	if control != "/tmp/autre.sock" || hook != "/tmp/autre-hook.sock" {
		t.Errorf("control = %q, hook = %q — the two must share one precedence rule, whichever it is",
			control, hook)
	}
}
