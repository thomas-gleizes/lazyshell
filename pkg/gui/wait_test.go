package gui

import (
	"testing"
	"time"

	"github.com/jesseduffield/gocui"
)

// Helpers shared by the tests that run a real gocui MainLoop: the two polling
// ones below, and the rule about when those tests may run at all.

// skipMainLoopUnderRace opts a test out of `go test -race`, and is called by
// every test that starts a real gocui MainLoop.
//
// The race is in gocui, not in lazyshell and not in the tests. Its headless
// driver keeps the simulation screen in a *package-level* variable
// (tcell_driver.go's `Screen = s`, written by every NewGui), and MainLoop's
// pollEvent goroutine is never joined when MainLoop returns. So a poller left
// over from one test reads that global while the next test's NewGui writes it.
// Both ends of the race are inside the library.
//
// That is also why it could not be pinned on any one test: the detector blames
// whichever test called NewGui after a leaked poller, so the name changed from
// run to run. The only rule that removes it is "under -race, never start a
// MainLoop".
//
// A skip rather than a build tag on the whole file: a skipped test says so in
// `go test -v`, an excluded file says nothing at all. These tests still run in
// CI's ordinary test job, where they carry the end-to-end coverage of the
// phase 3 and phase 4 exit criteria.
func skipMainLoopUnderRace(t *testing.T) {
	t.Helper()

	if raceEnabled {
		t.Skip("gocui's headless driver races with itself across MainLoops; see wait_test.go")
	}
}

// waitForCondition polls cond until it is true or the deadline passes —
// g.Update only enqueues work, it does not wait for MainLoop to process it,
// so state it mutates must be observed by polling, not by checking right
// after the call.
func waitForCondition(t *testing.T, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal(msg)
}

// waitForView polls until a view exists — the first layout pass runs
// asynchronously once MainLoop starts.
func waitForView(t *testing.T, g *gocui.Gui, name string) *gocui.View {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if view, err := g.View(name); err == nil {
			return view
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("view %q never appeared", name)

	return nil
}
