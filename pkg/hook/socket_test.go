package hook

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
)

// eventCollector is a thread-safe recorder for onEvent callbacks — Listen's
// accept loop and each connection handler run on their own goroutines.
type eventCollector struct {
	mu     sync.Mutex
	events []agent.State
}

func (c *eventCollector) record(s agent.State) {
	c.mu.Lock()
	c.events = append(c.events, s)
	c.mu.Unlock()
}

func (c *eventCollector) snapshot() []agent.State {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]agent.State(nil), c.events...)
}

// tempSocketPath returns a socket path under a short, explicit temp dir
// rather than t.TempDir(): that helper embeds the test's name in the path,
// and a Unix socket path is capped at ~104 bytes on macOS — long enough for
// t.TempDir() plus a descriptive test name to blow past it.
func tempSocketPath(t *testing.T, name string) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "hook")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return filepath.Join(dir, name)
}

func waitForEvents(t *testing.T, c *eventCollector, n int) []agent.State {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := c.snapshot(); len(got) >= n {
			return got
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d event(s), got %v", n, c.snapshot())

	return nil
}

func TestSocketPathIsUnderRuntimeDirAndPerSession(t *testing.T) {
	a := SocketPath("session-1")
	b := SocketPath("session-2")

	if a == b {
		t.Fatalf("SocketPath must be per-session, got the same path %q for two different ids", a)
	}

	if filepath.Base(a) != "session-1.sock" {
		t.Errorf("SocketPath(%q) = %q, want a file named after the session id", "session-1", a)
	}
}

func TestListenAndSendDeliversTheEvent(t *testing.T) {
	path := tempSocketPath(t, "s.sock")

	collector := &eventCollector{}
	srv, err := Listen(path, collector.record)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if err := Send(path, agent.StateBlocked); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := waitForEvents(t, collector, 1)
	if got[0] != agent.StateBlocked {
		t.Errorf("received %v, want StateBlocked", got[0])
	}
}

func TestListenSetsSocketPermissions(t *testing.T) {
	path := tempSocketPath(t, "perm.sock")

	srv, err := Listen(path, func(agent.State) {})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket permissions = %o, want 0600", perm)
	}
}

func TestInvalidLineIsIgnoredNotFatal(t *testing.T) {
	path := tempSocketPath(t, "s.sock")

	collector := &eventCollector{}
	srv, err := Listen(path, collector.record)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if _, err := conn.Write([]byte("not-a-real-state\nworking\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = conn.Close()

	got := waitForEvents(t, collector, 1)
	if len(got) != 1 || got[0] != agent.StateWorking {
		t.Fatalf("events = %v, want exactly [StateWorking] (the invalid line dropped, not fatal)", got)
	}
}

func TestLongLivedConnectionStreamsMultipleEvents(t *testing.T) {
	path := tempSocketPath(t, "s.sock")

	collector := &eventCollector{}
	srv, err := Listen(path, collector.record)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if _, err := conn.Write([]byte("working\nblocked\ndone\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = conn.Close()

	got := waitForEvents(t, collector, 3)
	want := []agent.State{agent.StateWorking, agent.StateBlocked, agent.StateDone}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("event %d = %v, want %v", i, got[i], w)
		}
	}
}

func TestCloseRemovesTheSocketFile(t *testing.T) {
	path := tempSocketPath(t, "s.sock")

	srv, err := Listen(path, func(agent.State) {})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket file missing right after Listen: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket file still present after Close: err = %v", err)
	}
}

func TestListenRemovesAStaleSocketFile(t *testing.T) {
	path := tempSocketPath(t, "s.sock")

	// Simulate a socket file left behind by an unclean previous exit: a
	// plain file at the same path, which a raw net.Listen would refuse to
	// bind over.
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv, err := Listen(path, func(agent.State) {})
	if err != nil {
		t.Fatalf("Listen over a stale file: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
}

func TestSendToAMissingSocketReturnsError(t *testing.T) {
	path := tempSocketPath(t, "does-not-exist.sock")

	if err := Send(path, agent.StateDone); err == nil {
		t.Fatal("Send to a missing socket returned nil, want an error")
	}
}
