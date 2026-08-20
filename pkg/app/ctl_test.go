package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/control"
)

// recordingHandler stands in for a running lazyshell: it records the request
// it was asked to serve, so the tests can assert on what actually went over
// the wire rather than on what the CLI printed.
type recordingHandler struct {
	mu   sync.Mutex
	last control.Request

	sessions []control.SessionInfo
	output   string
	newID    string
	count    int
	err      error
}

func (h *recordingHandler) record(req control.Request) {
	h.mu.Lock()
	h.last = req
	h.mu.Unlock()
}

func (h *recordingHandler) snapshot() control.Request {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.last
}

func (h *recordingHandler) List(group string) []control.SessionInfo {
	h.record(control.Request{Verb: control.VerbList, Group: group})

	return h.sessions
}

func (h *recordingHandler) Read(id string, tail int) (string, error) {
	h.record(control.Request{Verb: control.VerbRead, ID: id, Tail: tail})

	return h.output, h.err
}

func (h *recordingHandler) New(spec control.NewSpec) (string, error) {
	h.record(control.Request{
		Verb: control.VerbNew, Name: spec.Name, Cwd: spec.Cwd, Command: spec.Command, Group: spec.Group,
	})

	return h.newID, h.err
}

func (h *recordingHandler) Send(id, text string) error {
	h.record(control.Request{Verb: control.VerbSend, ID: id, Text: text})

	return h.err
}

func (h *recordingHandler) Kill(id string) error {
	h.record(control.Request{Verb: control.VerbKill, ID: id})

	return h.err
}

func (h *recordingHandler) SetGroup(id, group string) error {
	h.record(control.Request{Verb: control.VerbGroup, ID: id, Group: group})

	return h.err
}

func (h *recordingHandler) GroupSend(group, text string) (int, error) {
	h.record(control.Request{Verb: control.VerbGroupSend, Group: group, Text: text})

	return h.count, h.err
}

func (h *recordingHandler) GroupKill(group string) (int, error) {
	h.record(control.Request{Verb: control.VerbGroupKill, Group: group})

	return h.count, h.err
}

func (h *recordingHandler) Rename(id, name string) error {
	h.record(control.Request{Verb: control.VerbRename, ID: id, Name: name})

	return h.err
}

func (h *recordingHandler) Wait(idOrName, group, state string, timeout time.Duration) (control.SessionInfo, error) {
	h.record(control.Request{
		Verb: control.VerbWait, ID: idOrName, Group: group, State: state, Timeout: int(timeout.Seconds()),
	})

	if len(h.sessions) > 0 {
		return h.sessions[0], h.err
	}

	return control.SessionInfo{}, h.err
}

// startCtlServer stands a control socket up and points $LAZYSHELL_CONTROL_SOCK
// at it, which is how RunCtl finds it inside a session. The short MkdirTemp is
// pkg/control's rule: a Unix socket path is capped at ~104 bytes.
func startCtlServer(t *testing.T, h control.Handler) {
	t.Helper()

	dir, err := os.MkdirTemp("", "ctl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "c.sock")

	srv, err := control.Listen(path, h)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	t.Setenv("LAZYSHELL_CONTROL_SOCK", path)
}

// runCtl parses a command line and runs it, the way `lazyshell ctl ...` does.
func runCtl(t *testing.T, args ...string) (string, error) {
	t.Helper()

	inv, err := ParseArgs(append([]string{"ctl"}, args...))
	if err != nil {
		t.Fatalf("ParseArgs(%v): %v", args, err)
	}

	var out bytes.Buffer

	runErr := RunCtl(inv, &out)

	return out.String(), runErr
}

func TestParseArgsCtlVerbsAndFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantVerb string
		wantArgs []string
		check    func(*testing.T, Invocation)
	}{
		{
			name:     "list",
			args:     []string{"ctl", "list"},
			wantVerb: control.VerbList,
		},
		{
			name:     "read with tail after the target",
			args:     []string{"ctl", "read", "session-1", "--tail", "20"},
			wantVerb: control.VerbRead,
			wantArgs: []string{"session-1"},
			check: func(t *testing.T, inv Invocation) {
				if inv.Ctl.Tail != 20 {
					t.Errorf("Tail = %d, want 20", inv.Ctl.Tail)
				}
			},
		},
		{
			name:     "new with every flag",
			args:     []string{"ctl", "new", "--name", "worker", "--cwd", "/tmp", "--command", "sleep 1"},
			wantVerb: control.VerbNew,
			check: func(t *testing.T, inv Invocation) {
				if inv.Ctl.Name != "worker" || inv.Ctl.Cwd != "/tmp" || inv.Ctl.Command != "sleep 1" {
					t.Errorf("Ctl = %+v", inv.Ctl)
				}
			},
		},
		{
			// The reason ctl parses on its own: a flag after two positionals.
			name:     "send with unquoted text and a trailing flag",
			args:     []string{"ctl", "send", "session-1", "echo", "bonjour", "--enter"},
			wantVerb: control.VerbSend,
			wantArgs: []string{"session-1", "echo", "bonjour"},
			check: func(t *testing.T, inv Invocation) {
				if !inv.Ctl.Enter {
					t.Error("--enter after the positionals was not parsed")
				}
			},
		},
		{
			name:     "rename",
			args:     []string{"ctl", "rename", "session-1", "chef"},
			wantVerb: control.VerbRename,
			wantArgs: []string{"session-1", "chef"},
		},
		{
			name:     "json",
			args:     []string{"ctl", "list", "--json"},
			wantVerb: control.VerbList,
			check: func(t *testing.T, inv Invocation) {
				if !inv.Ctl.JSON {
					t.Error("--json was not parsed")
				}
			},
		},
		{
			name:     "wait on a single session",
			args:     []string{"ctl", "wait", "session-1", "--state", "blocked", "--timeout", "30"},
			wantVerb: control.VerbWait,
			wantArgs: []string{"session-1"},
			check: func(t *testing.T, inv Invocation) {
				if inv.Ctl.State != "blocked" || inv.Ctl.Timeout != 30 {
					t.Errorf("Ctl = %+v", inv.Ctl)
				}
			},
		},
		{
			name:     "wait on a group with no explicit timeout",
			args:     []string{"ctl", "wait", "--group", "agents", "--state", "done"},
			wantVerb: control.VerbWait,
			check: func(t *testing.T, inv Invocation) {
				if inv.Ctl.Group != "agents" || inv.Ctl.State != "done" || inv.Ctl.Timeout != 0 {
					t.Errorf("Ctl = %+v", inv.Ctl)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := ParseArgs(tt.args)
			if err != nil {
				t.Fatalf("ParseArgs: %v", err)
			}

			if inv.Command != CommandCtl {
				t.Fatalf("Command = %q, want %q", inv.Command, CommandCtl)
			}

			if inv.CtlVerb != tt.wantVerb {
				t.Errorf("CtlVerb = %q, want %q", inv.CtlVerb, tt.wantVerb)
			}

			if strings.Join(inv.CtlArgs, "|") != strings.Join(tt.wantArgs, "|") {
				t.Errorf("CtlArgs = %v, want %v", inv.CtlArgs, tt.wantArgs)
			}

			if tt.check != nil {
				tt.check(t, inv)
			}
		})
	}
}

// A typo must produce the usage message, not a request sent to a running
// lazyshell — the same rule `lazyshell config sho` follows.
func TestParseArgsCtlRejectsTyposAndWrongArity(t *testing.T) {
	tests := [][]string{
		{"ctl"},
		{"ctl", "danse"},
		{"ctl", "read"},
		{"ctl", "read", "a", "b"},
		{"ctl", "send", "session-1"},
		{"ctl", "rename", "session-1"},
		{"ctl", "list", "de", "trop"},
		{"ctl", "group", "session-1"},
		{"ctl", "ungroup"},
		{"ctl", "group-send", "agents"},
		{"ctl", "group-kill"},
		{"ctl", "wait", "session-1"},          // missing --state
		{"ctl", "wait", "--group", "agents"},  // missing --state
		{"ctl", "wait", "--state", "blocked"}, // neither session nor --group
		{"ctl", "wait", "session-1", "--group", "agents", "--state", "blocked"}, // both
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := ParseArgs(args); err == nil {
				t.Errorf("ParseArgs(%v) returned no error", args)
			}
		})
	}
}

func TestRunCtlSendsTheRightRequestForEachVerb(t *testing.T) {
	h := &recordingHandler{
		sessions: []control.SessionInfo{{ID: "session-1", Name: "chef", Status: "running", AgentState: "working"}},
		output:   "sortie-de-session\n",
		newID:    "session-9",
	}
	startCtlServer(t, h)

	t.Run("list prints a line per session", func(t *testing.T) {
		out, err := runCtl(t, "list")
		if err != nil {
			t.Fatalf("RunCtl: %v", err)
		}

		if !strings.Contains(out, "session-1") || !strings.Contains(out, "chef") ||
			!strings.Contains(out, "working") {
			t.Errorf("list output = %q", out)
		}
	})

	t.Run("read prints the output raw", func(t *testing.T) {
		out, err := runCtl(t, "read", "session-1", "--tail", "5")
		if err != nil {
			t.Fatalf("RunCtl: %v", err)
		}

		if out != "sortie-de-session\n" {
			t.Errorf("read output = %q, want it unwrapped", out)
		}

		if got := h.snapshot(); got.ID != "session-1" || got.Tail != 5 {
			t.Errorf("request = %+v", got)
		}
	})

	t.Run("new prints the created id", func(t *testing.T) {
		out, err := runCtl(t, "new", "--name", "worker", "--command", "sleep 1")
		if err != nil {
			t.Fatalf("RunCtl: %v", err)
		}

		if strings.TrimSpace(out) != "session-9" {
			t.Errorf("new output = %q, want the created id", out)
		}

		if got := h.snapshot(); got.Name != "worker" || got.Command != "sleep 1" {
			t.Errorf("request = %+v", got)
		}
	})

	t.Run("send joins the words and honours --enter", func(t *testing.T) {
		if _, err := runCtl(t, "send", "session-1", "echo", "bonjour", "--enter"); err != nil {
			t.Fatalf("RunCtl: %v", err)
		}

		if got := h.snapshot(); got.Text != "echo bonjour\r" {
			t.Errorf("text = %q, want %q", got.Text, "echo bonjour\\r")
		}
	})

	t.Run("send without --enter types nothing extra", func(t *testing.T) {
		if _, err := runCtl(t, "send", "session-1", "sans-entree"); err != nil {
			t.Fatalf("RunCtl: %v", err)
		}

		if got := h.snapshot(); got.Text != "sans-entree" {
			t.Errorf("text = %q, want no trailing carriage return", got.Text)
		}
	})

	t.Run("kill and rename", func(t *testing.T) {
		if _, err := runCtl(t, "kill", "session-1"); err != nil {
			t.Fatalf("kill: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbKill || got.ID != "session-1" {
			t.Errorf("kill request = %+v", got)
		}

		if _, err := runCtl(t, "rename", "session-1", "chef2"); err != nil {
			t.Fatalf("rename: %v", err)
		}

		if got := h.snapshot(); got.Name != "chef2" {
			t.Errorf("rename request = %+v", got)
		}
	})

	t.Run("group assigns, ungroup clears", func(t *testing.T) {
		if _, err := runCtl(t, "group", "session-1", "agents"); err != nil {
			t.Fatalf("group: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbGroup || got.ID != "session-1" || got.Group != "agents" {
			t.Errorf("group request = %+v", got)
		}

		// The alias collapses to the same verb with an empty group — there is
		// no second verb on the wire.
		if _, err := runCtl(t, "ungroup", "session-1"); err != nil {
			t.Fatalf("ungroup: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbGroup || got.Group != "" {
			t.Errorf("ungroup request = %+v, want VerbGroup with no group", got)
		}
	})

	t.Run("--group filters list and sets a new session's group", func(t *testing.T) {
		if _, err := runCtl(t, "list", "--group", "agents"); err != nil {
			t.Fatalf("list: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbList || got.Group != "agents" {
			t.Errorf("list request = %+v", got)
		}

		if _, err := runCtl(t, "new", "--name", "w1", "--group", "agents"); err != nil {
			t.Fatalf("new: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbNew || got.Group != "agents" {
			t.Errorf("new request = %+v", got)
		}
	})

	t.Run("group-send joins the words and group-kill reports a count", func(t *testing.T) {
		h.count = 3

		out, err := runCtl(t, "group-send", "agents", "echo", "bonjour", "--enter")
		if err != nil {
			t.Fatalf("group-send: %v", err)
		}

		if got := h.snapshot(); got.Group != "agents" || got.Text != "echo bonjour\r" {
			t.Errorf("group-send request = %+v", got)
		}
		if !strings.Contains(out, "3") {
			t.Errorf("group-send output = %q, want the count of sessions reached", out)
		}

		out, err = runCtl(t, "group-kill", "agents")
		if err != nil {
			t.Fatalf("group-kill: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbGroupKill || got.Group != "agents" {
			t.Errorf("group-kill request = %+v", got)
		}
		if !strings.Contains(out, "3") {
			t.Errorf("group-kill output = %q, want the count of sessions killed", out)
		}
	})

	t.Run("wait sends the target, state and timeout, and prints the matched session", func(t *testing.T) {
		h.sessions = []control.SessionInfo{{ID: "session-1", Name: "chef", Status: "running", AgentState: "blocked"}}

		out, err := runCtl(t, "wait", "session-1", "--state", "blocked", "--timeout", "5")
		if err != nil {
			t.Fatalf("wait: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbWait || got.ID != "session-1" ||
			got.State != "blocked" || got.Timeout != 5 {
			t.Errorf("wait request = %+v", got)
		}

		if !strings.Contains(out, "session-1") || !strings.Contains(out, "blocked") {
			t.Errorf("wait output = %q, want the matched session's line", out)
		}
	})

	t.Run("wait --group sends the group, not an id", func(t *testing.T) {
		if _, err := runCtl(t, "wait", "--group", "agents", "--state", "done"); err != nil {
			t.Fatalf("wait --group: %v", err)
		}

		if got := h.snapshot(); got.Verb != control.VerbWait || got.ID != "" || got.Group != "agents" || got.State != "done" {
			t.Errorf("wait --group request = %+v", got)
		}
	})

	t.Run("list shows a session's group", func(t *testing.T) {
		h.sessions = []control.SessionInfo{
			{ID: "session-1", Name: "chef", Status: "running", Group: "agents"},
		}

		out, err := runCtl(t, "list")
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if !strings.Contains(out, "agents") {
			t.Errorf("list output = %q, want the group shown", out)
		}
	})
}

// The headline difference with `lazyshell hook`, which always exits 0: an
// agent that asked for something and did not get it has to be able to tell.
func TestRunCtlExitsNonZeroWhenTheVerbIsRefused(t *testing.T) {
	startCtlServer(t, &recordingHandler{err: errors.New("session inconnue")})

	_, err := runCtl(t, "kill", "fantome")
	if err == nil {
		t.Fatal("RunCtl returned nil for a refused verb")
	}

	if !strings.Contains(err.Error(), "session inconnue") {
		t.Errorf("error = %v, want it to carry lazyshell's own message", err)
	}
}

// --json changes the rendering, not the outcome.
func TestRunCtlJSONStillFailsOnARefusedVerb(t *testing.T) {
	startCtlServer(t, &recordingHandler{err: errors.New("session inconnue")})

	inv, err := ParseArgs([]string{"ctl", "kill", "fantome", "--json"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}

	var out bytes.Buffer

	if err := RunCtl(inv, &out); err == nil {
		t.Error("RunCtl --json returned nil for a refused verb")
	}

	if !strings.Contains(out.String(), `"ok": false`) {
		t.Errorf("--json output = %q, want the raw response", out.String())
	}
}

func TestRunCtlReportsThatTheAPIIsOffInsideASession(t *testing.T) {
	t.Setenv("LAZYSHELL_CONTROL_SOCK", "")
	t.Setenv("LAZYSHELL_SESSION_ID", "session-1")

	inv, err := ParseArgs([]string{"ctl", "list"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}

	err = RunCtl(inv, &bytes.Buffer{})
	if err == nil {
		t.Fatal("RunCtl succeeded with no control socket")
	}

	if !strings.Contains(err.Error(), "désactivée") {
		t.Errorf("error = %v, want it to name the disabled feature rather than a missing file", err)
	}
}

func TestRunCtlFailsWhenNothingIsListening(t *testing.T) {
	dir, err := os.MkdirTemp("", "ctl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	t.Setenv("LAZYSHELL_CONTROL_SOCK", filepath.Join(dir, "absent.sock"))

	if _, err := runCtl(t, "list"); err == nil {
		t.Fatal("RunCtl succeeded against a socket nothing is listening on")
	}
}
