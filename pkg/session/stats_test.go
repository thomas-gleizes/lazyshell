package session

import (
	"slices"
	"testing"
	"time"
)

func TestEnvReportsTheLaunchEnvironment(t *testing.T) {
	m := newTestManager(t)

	sess, err := m.NewWithOptions(Options{
		Name:  "api",
		Shell: testShell,
		Env:   map[string]string{"LAZYSHELL_TEST_PORT": "3000"},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	env := sess.Env()

	// The declarative entry, plus the identity buildEnv forces on every
	// session: both must be there, since the panel showing this is meant to
	// answer "what was this shell actually started with".
	for _, want := range []string{
		"LAZYSHELL_TEST_PORT=3000",
		"LAZYSHELL_SESSION_ID=" + sess.ID,
	} {
		if !slices.Contains(env, want) {
			t.Errorf("Env() does not contain %q", want)
		}
	}
}

// Env hands out a copy: a caller that mangles the slice must not be able to
// change what the process was started with, nor what a later reader sees.
func TestEnvIsACopy(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "shell")

	first := sess.Env()
	if len(first) == 0 {
		t.Fatal("Env() is empty, want the inherited environment")
	}

	first[0] = "CLOBBERED=yes"

	if got := sess.Env()[0]; got == "CLOBBERED=yes" {
		t.Error("Env() returned the live slice, want a copy")
	}
}

func TestStatsSamplesTheShellProcess(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "shell")

	// The shell has to have been scheduled at least once for its numbers to
	// mean anything; waiting on a prompt is the same synchronisation every
	// other test in this package uses.
	if _, err := sess.Write([]byte("echo READY\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForScreen(t, sess, "READY")

	stats, err := sess.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if len(stats) == 0 {
		t.Fatal("Stats() returned nothing, want at least the shell")
	}

	shell := stats[0]

	if got, want := shell.PID, sess.Cmd.Process.Pid; got != want {
		t.Errorf("stats[0].PID = %d, want the shell's %d", got, want)
	}

	if shell.Foreground {
		t.Error("stats[0].Foreground = true, want the shell's own sample first")
	}

	if shell.Comm == "" {
		t.Error("stats[0].Comm is empty, want the process name")
	}

	if shell.RSSBytes == 0 {
		t.Error("stats[0].RSSBytes = 0, want a running process to hold memory")
	}

	if shell.CPUTime < 0 {
		t.Errorf("stats[0].CPUTime = %v, want a non-negative cumulative time", shell.CPUTime)
	}

	if shell.SampledAt.IsZero() {
		t.Error("stats[0].SampledAt is zero, want the sampling instant")
	}
}

// The second sample is the whole point of the perf tab: what the user watches
// is the command in the foreground, not the shell that spawned it.
func TestStatsIncludesTheForegroundProcess(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "shell")

	if _, err := sess.Write([]byte("sleep 30\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	shellPID := sess.Cmd.Process.Pid

	// The foreground group only changes once the shell has actually forked,
	// which is not observable from here — poll rather than sleep a guess.
	deadline := time.Now().Add(10 * time.Second)
	for {
		stats, err := sess.Stats()
		if err == nil && len(stats) == 2 {
			if stats[1].PID == shellPID {
				t.Fatalf("stats[1].PID = %d, want a different process than the shell", shellPID)
			}

			if !stats[1].Foreground {
				t.Error("stats[1].Foreground = false, want the foreground sample marked")
			}

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for a foreground sample (last: %+v, err: %v)", stats, err)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// An exited session's pid may already have been reused, so reporting numbers
// for it would mean reporting a stranger's.
func TestStatsOnAnExitedSessionErrors(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "shell")

	if _, err := sess.Write([]byte("exit\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForStatus(t, sess, StatusExited)

	if _, err := sess.Stats(); err == nil {
		t.Error("Stats() on an exited session returned no error, want one")
	}
}
