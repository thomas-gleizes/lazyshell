package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/hook"
)

// RunHook's whole contract: it must never fail the process that called it,
// no matter what went wrong.
func TestRunHookNeverReturnsAnError(t *testing.T) {
	t.Setenv("LAZYSHELL_SOCK", "")

	var errOut bytes.Buffer
	if err := RunHook("working", &errOut); err != nil {
		t.Fatalf("RunHook with no $LAZYSHELL_SOCK returned an error: %v", err)
	}

	if !strings.Contains(errOut.String(), "LAZYSHELL_SOCK") {
		t.Errorf("errOut = %q, want a diagnostic mentioning $LAZYSHELL_SOCK", errOut.String())
	}
}

func TestRunHookInvalidEventDegradesGracefully(t *testing.T) {
	t.Setenv("LAZYSHELL_SOCK", filepath.Join(t.TempDir(), "s.sock"))

	var errOut bytes.Buffer
	if err := RunHook("not-a-real-state", &errOut); err != nil {
		t.Fatalf("RunHook with an invalid event returned an error: %v", err)
	}

	if !strings.Contains(errOut.String(), "not-a-real-state") {
		t.Errorf("errOut = %q, want a diagnostic naming the bad event", errOut.String())
	}
}

func TestRunHookMissingSocketDegradesGracefully(t *testing.T) {
	t.Setenv("LAZYSHELL_SOCK", filepath.Join(t.TempDir(), "does-not-exist.sock"))

	var errOut bytes.Buffer
	if err := RunHook("done", &errOut); err != nil {
		t.Fatalf("RunHook against a missing socket returned an error: %v", err)
	}

	if errOut.Len() == 0 {
		t.Error("errOut is empty, want a diagnostic about the failed send")
	}
}

// End-to-end: a real hook.Server standing in for the session's listener,
// with $LAZYSHELL_SOCK pointed at it — this is the same path a session's
// hook config exercises, minus the pty.
func TestRunHookDeliversToARealServer(t *testing.T) {
	dir, err := os.MkdirTemp("", "hooktest")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "s.sock")

	received := make(chan agent.State, 1)
	srv, err := hook.Listen(sockPath, func(s agent.State) { received <- s })
	if err != nil {
		t.Fatalf("hook.Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	t.Setenv("LAZYSHELL_SOCK", sockPath)

	var errOut bytes.Buffer
	if err := RunHook("blocked", &errOut); err != nil {
		t.Fatalf("RunHook: %v", err)
	}

	select {
	case got := <-received:
		if got != agent.StateBlocked {
			t.Errorf("received %v, want StateBlocked", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the event")
	}

	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want empty on success", errOut.String())
	}
}
