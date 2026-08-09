package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHandler records every call and answers from canned values, so the
// protocol can be exercised with no session manager and no interface.
type fakeHandler struct {
	mu    sync.Mutex
	calls []string

	sessions []SessionInfo
	output   string
	newID    string
	count    int
	err      error
}

func (h *fakeHandler) record(format string, args ...any) {
	h.mu.Lock()
	h.calls = append(h.calls, fmt.Sprintf(format, args...))
	h.mu.Unlock()
}

func (h *fakeHandler) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.calls...)
}

func (h *fakeHandler) List(group string) []SessionInfo {
	h.record("list %s", group)

	return h.sessions
}

func (h *fakeHandler) Read(id string, tail int) (string, error) {
	h.record("read %s %d", id, tail)

	return h.output, h.err
}

func (h *fakeHandler) New(spec NewSpec) (string, error) {
	h.record("new %s %s %s %s", spec.Name, spec.Cwd, spec.Command, spec.Group)

	return h.newID, h.err
}

func (h *fakeHandler) Send(id, text string) error {
	h.record("send %s %q", id, text)

	return h.err
}

func (h *fakeHandler) Kill(id string) error {
	h.record("kill %s", id)

	return h.err
}

func (h *fakeHandler) Rename(id, name string) error {
	h.record("rename %s %s", id, name)

	return h.err
}

func (h *fakeHandler) SetGroup(id, group string) error {
	h.record("group %s %s", id, group)

	return h.err
}

func (h *fakeHandler) GroupSend(group, text string) (int, error) {
	h.record("group-send %s %q", group, text)

	return h.count, h.err
}

func (h *fakeHandler) GroupKill(group string) (int, error) {
	h.record("group-kill %s", group)

	return h.count, h.err
}

// tempSocketPath returns a socket path under a short, explicit temp dir rather
// than t.TempDir(), for the reason pkg/hook's identically named helper gives:
// that helper embeds the test's name in the path and a Unix socket path is
// capped at ~104 bytes on macOS.
func tempSocketPath(t *testing.T, name string) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ctl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return filepath.Join(dir, name)
}

func startServer(t *testing.T, h Handler) string {
	t.Helper()

	path := tempSocketPath(t, "c.sock")

	srv, err := Listen(path, h)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	return path
}

func TestEveryVerbReachesTheHandlerAndAnswers(t *testing.T) {
	h := &fakeHandler{
		sessions: []SessionInfo{{ID: "session-1", Name: "chef", Status: "running"}},
		output:   "bonjour\n",
		newID:    "session-2",
		count:    3,
	}
	path := startServer(t, h)

	tests := []struct {
		name string
		req  Request
		want func(Response) error
	}{
		{
			name: "list",
			req:  Request{Verb: VerbList},
			want: func(r Response) error {
				if len(r.Sessions) != 1 || r.Sessions[0].Name != "chef" {
					return fmt.Errorf("sessions = %+v", r.Sessions)
				}

				return nil
			},
		},
		{
			name: "read",
			req:  Request{Verb: VerbRead, ID: "session-1", Tail: 5},
			want: func(r Response) error {
				if r.Output != "bonjour\n" {
					return fmt.Errorf("output = %q", r.Output)
				}

				return nil
			},
		},
		{
			name: "new",
			req:  Request{Verb: VerbNew, Name: "worker", Cwd: "/tmp", Command: "sleep 1"},
			want: func(r Response) error {
				if r.ID != "session-2" {
					return fmt.Errorf("id = %q", r.ID)
				}

				return nil
			},
		},
		{name: "send", req: Request{Verb: VerbSend, ID: "session-1", Text: "echo hi\r"}},
		{name: "kill", req: Request{Verb: VerbKill, ID: "session-1"}},
		{name: "rename", req: Request{Verb: VerbRename, ID: "session-1", Name: "chef2"}},
		{name: "group", req: Request{Verb: VerbGroup, ID: "session-1", Group: "agents"}},
		{
			name: "group-send",
			req:  Request{Verb: VerbGroupSend, Group: "agents", Text: "ls\r"},
			want: func(r Response) error {
				if r.Count != 3 {
					return fmt.Errorf("count = %d, want the handler's 3", r.Count)
				}

				return nil
			},
		},
		{
			name: "group-kill",
			req:  Request{Verb: VerbGroupKill, Group: "agents"},
			want: func(r Response) error {
				if r.Count != 3 {
					return fmt.Errorf("count = %d, want the handler's 3", r.Count)
				}

				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Call(path, tt.req)
			if err != nil {
				t.Fatalf("Call: %v", err)
			}

			if !resp.OK {
				t.Fatalf("OK = false, error = %q", resp.Error)
			}

			if tt.want != nil {
				if err := tt.want(resp); err != nil {
					t.Error(err)
				}
			}
		})
	}

	want := []string{
		"list ",
		"read session-1 5",
		"new worker /tmp sleep 1 ",
		`send session-1 "echo hi\r"`,
		"kill session-1",
		"rename session-1 chef2",
		"group session-1 agents",
		`group-send agents "ls\r"`,
		"group-kill agents",
	}

	got := h.snapshot()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Exit code 0 is the answer a caller most often waits for — "did the build
// pass?" — and it is exactly the value a plain `int` with omitempty throws
// away. Found by an agent driving the real API, not by the tests, which is why
// it is pinned at the wire level rather than at the Go one.
func TestExitCodeZeroSurvivesSerialisation(t *testing.T) {
	zero, one := 0, 1
	h := &fakeHandler{sessions: []SessionInfo{
		{ID: "session-1", Status: "running"},
		{ID: "session-2", Status: "exited", ExitCode: &zero},
		{ID: "session-3", Status: "exited", ExitCode: &one},
	}}

	resp, err := Call(startServer(t, h), Request{Verb: VerbList})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if got := resp.Sessions[0].ExitCode; got != nil {
		t.Errorf("running session reported exit code %d, want none", *got)
	}

	if got := resp.Sessions[1].ExitCode; got == nil || *got != 0 {
		t.Errorf("exit code 0 came back as %v, want 0 — success must be distinguishable from \"still running\"", got)
	}

	if got := resp.Sessions[2].ExitCode; got == nil || *got != 1 {
		t.Errorf("exit code 1 came back as %v, want 1", got)
	}
}

func TestHandlerErrorBecomesAResponseNotATransportError(t *testing.T) {
	h := &fakeHandler{err: errors.New("session inconnue")}
	path := startServer(t, h)

	resp, err := Call(path, Request{Verb: VerbKill, ID: "nope"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if resp.OK {
		t.Fatal("OK = true, want false")
	}

	if resp.Error != "session inconnue" {
		t.Errorf("Error = %q", resp.Error)
	}
}

// An agent that guesses a verb name, or writes something that is not JSON at
// all, must get an answer and keep the connection — that is the whole
// difference between a degraded call and a lost channel.
func TestBadRequestsAnswerAndLeaveTheConnectionUsable(t *testing.T) {
	h := &fakeHandler{sessions: []SessionInfo{{ID: "session-1"}}}
	path := startServer(t, h)

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	scanner := bufio.NewScanner(conn)
	send := func(line string) Response {
		t.Helper()

		if _, err := conn.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}

		if !scanner.Scan() {
			t.Fatalf("no answer to %q: %v", line, scanner.Err())
		}

		var resp Response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal answer to %q: %v", line, err)
		}

		return resp
	}

	if resp := send(`{"verb":"danse"}`); resp.OK || !strings.Contains(resp.Error, "verbe inconnu") {
		t.Errorf("unknown verb: %+v", resp)
	}

	if resp := send(`pas du json`); resp.OK || !strings.Contains(resp.Error, "illisible") {
		t.Errorf("garbage: %+v", resp)
	}

	// The point of the test: the connection still works afterwards.
	if resp := send(`{"verb":"list"}`); !resp.OK || len(resp.Sessions) != 1 {
		t.Errorf("list after bad requests: %+v", resp)
	}
}

func TestSocketIsCreatedPrivateAndRemovedOnClose(t *testing.T) {
	path := tempSocketPath(t, "c.sock")

	srv, err := Listen(path, &fakeHandler{})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600 — the socket is this API's only access control", perm)
	}

	if srv.Path() != path {
		t.Errorf("Path() = %q, want %q", srv.Path(), path)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket still there after Close: %v", err)
	}
}

// Listen has to survive a socket file left behind by an unclean exit, or a
// crashed lazyshell would poison every later run.
func TestListenOverAStaleSocketFile(t *testing.T) {
	path := tempSocketPath(t, "c.sock")

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv, err := Listen(path, &fakeHandler{})
	if err != nil {
		t.Fatalf("Listen over a stale file: %v", err)
	}

	_ = srv.Close()
}

func TestConcurrentConnections(t *testing.T) {
	h := &fakeHandler{sessions: []SessionInfo{{ID: "session-1"}}}
	path := startServer(t, h)

	const n = 8

	var wg sync.WaitGroup

	errs := make(chan error, n)

	for range n {
		wg.Add(1)

		go func() {
			defer wg.Done()

			resp, err := Call(path, Request{Verb: VerbList})
			switch {
			case err != nil:
				errs <- err
			case !resp.OK:
				errs <- errors.New(resp.Error)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Call: %v", err)
	}

	if got := len(h.snapshot()); got != n {
		t.Errorf("handler called %d times, want %d", got, n)
	}
}

func TestCallOnAMissingSocketFails(t *testing.T) {
	if _, err := Call(tempSocketPath(t, "absent.sock"), Request{Verb: VerbList}); err == nil {
		t.Fatal("Call on a missing socket returned no error — `lazyshell ctl` must be able to exit non-zero")
	}
}
