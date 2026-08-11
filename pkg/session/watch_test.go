package session

import (
	"testing"
	"time"
)

// White-box throughout (same package), same spirit as agent_test.go's direct
// evaluateAgentState calls: feedWatch is called by hand with arbitrary bytes
// rather than typed into a real shell, so line-splitting and cooldown
// behaviour are deterministic instead of racing shell startup timing.

func TestFeedWatchMatchesCompleteLine(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-match")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	sess.feedWatch([]byte("build output\nERR! something broke\n"))

	hit, ok := sess.LastWatchHit()
	if !ok {
		t.Fatal("LastWatchHit() ok = false after a matching line")
	}
	if hit.Pattern != "ERR!" {
		t.Errorf("hit.Pattern = %q, want %q", hit.Pattern, "ERR!")
	}
	if hit.Line != "ERR! something broke" {
		t.Errorf("hit.Line = %q, want %q", hit.Line, "ERR! something broke")
	}
	if !hit.Notify {
		t.Error("hit.Notify = false, want true for a runtime-armed watch")
	}
}

func TestFeedWatchPartialLineAcrossWrites(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-partial")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	sess.feedWatch([]byte("ER"))
	if _, ok := sess.LastWatchHit(); ok {
		t.Fatal("LastWatchHit() ok = true before the line's newline arrived")
	}

	sess.feedWatch([]byte("R! rest of the line\n"))
	if _, ok := sess.LastWatchHit(); !ok {
		t.Fatal("LastWatchHit() ok = false once the line completed across writes")
	}
}

func TestFeedWatchStripsAnsi(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-ansi")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	sess.feedWatch([]byte("\x1b[31mERR!\x1b[0m colored\n"))

	if _, ok := sess.LastWatchHit(); !ok {
		t.Fatal("LastWatchHit() ok = false for a match hidden behind ANSI color codes")
	}
}

func TestFeedWatchCooldownDropsRepeatWithinWindow(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-cooldown")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	sess.feedWatch([]byte("ERR! first\n"))
	first, ok := sess.LastWatchHit()
	if !ok {
		t.Fatal("LastWatchHit() ok = false after the first match")
	}

	sess.feedWatch([]byte("ERR! second, still inside the cooldown window\n"))
	second, ok := sess.LastWatchHit()
	if !ok {
		t.Fatal("LastWatchHit() ok = false after the second match")
	}
	if second.Seq != first.Seq {
		t.Fatalf("Seq advanced to %d within watchCooldown, want it to stay at %d", second.Seq, first.Seq)
	}

	// Backdate the armed watch's cooldown instead of sleeping out the real
	// 3s window — same reasoning as everywhere else in this package that
	// favours a deterministic assertion over a slow one.
	sess.watchMu.Lock()
	sess.runtimeWatch.lastFired = time.Now().Add(-watchCooldown - time.Millisecond)
	sess.watchMu.Unlock()

	sess.feedWatch([]byte("ERR! third, cooldown has elapsed\n"))
	third, ok := sess.LastWatchHit()
	if !ok {
		t.Fatal("LastWatchHit() ok = false after the third match")
	}
	if third.Seq != first.Seq+1 {
		t.Fatalf("Seq = %d after the cooldown elapsed, want %d", third.Seq, first.Seq+1)
	}
}

func TestFeedWatchLineBufCap(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-cap")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	// No newline at all: past watchLineBufMax bytes, feedWatch must flush and
	// match what it has rather than growing lineBuf without bound.
	long := make([]byte, watchLineBufMax+1)
	for i := range long {
		long[i] = 'x'
	}
	copy(long, "ERR!")

	sess.feedWatch(long)

	if _, ok := sess.LastWatchHit(); !ok {
		t.Fatal("LastWatchHit() ok = false after exceeding watchLineBufMax with no newline")
	}

	sess.watchMu.Lock()
	bufLen := len(sess.lineBuf)
	sess.watchMu.Unlock()

	if bufLen != 0 {
		t.Fatalf("lineBuf len = %d after the cap flush, want 0", bufLen)
	}
}

func TestFeedWatchSkipsAndResetsOnAltScreen(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-altscreen")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	// A partial line buffered right before entering alt-screen must never
	// stitch to bytes written after leaving it.
	sess.feedWatch([]byte("ER"))

	if _, err := sess.screen.Write([]byte("\x1b[?1049h")); err != nil {
		t.Fatalf("entering alt screen: %v", err)
	}

	sess.feedWatch([]byte("R! this must be ignored, alt screen is active\n"))
	if _, ok := sess.LastWatchHit(); ok {
		t.Fatal("LastWatchHit() ok = true for output written while alt-screen is active")
	}

	if _, err := sess.screen.Write([]byte("\x1b[?1049l")); err != nil {
		t.Fatalf("leaving alt screen: %v", err)
	}

	sess.feedWatch([]byte("ERR! after returning from alt screen\n"))
	hit, ok := sess.LastWatchHit()
	if !ok {
		t.Fatal("LastWatchHit() ok = false after returning from alt screen")
	}
	if hit.Line != "ERR! after returning from alt screen" {
		t.Errorf("hit.Line = %q, stale buffer was not reset across the alt-screen transition", hit.Line)
	}
}

func TestArmWatchEmptyClearsRuntimeWatch(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-clear")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}
	if got := sess.RuntimeWatchPattern(); got != "ERR!" {
		t.Fatalf("RuntimeWatchPattern() = %q, want %q", got, "ERR!")
	}

	if err := sess.ArmWatch(""); err != nil {
		t.Fatalf("ArmWatch(\"\"): %v", err)
	}
	if got := sess.RuntimeWatchPattern(); got != "" {
		t.Fatalf("RuntimeWatchPattern() = %q after disarming, want \"\"", got)
	}

	sess.feedWatch([]byte("ERR! must not match, nothing is armed\n"))
	if _, ok := sess.LastWatchHit(); ok {
		t.Fatal("LastWatchHit() ok = true after the runtime watch was cleared")
	}
}

func TestArmWatchInvalidPatternIsRejectedAndLeavesPreviousArmed(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-invalid")

	if err := sess.ArmWatch("ERR!"); err != nil {
		t.Fatalf("ArmWatch: %v", err)
	}

	if err := sess.ArmWatch("["); err == nil {
		t.Fatal("ArmWatch(\"[\") err = nil, want an error for an invalid regexp")
	}

	if got := sess.RuntimeWatchPattern(); got != "ERR!" {
		t.Fatalf("RuntimeWatchPattern() = %q after a rejected re-arm, want the previous pattern %q kept", got, "ERR!")
	}
}

func TestSetConfigWatchersNotifyFlagCarriesToHit(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-config-notify")

	sess.setConfigWatchers([]WatchSpec{{Pattern: "silent", Notify: false}})
	sess.feedWatch([]byte("silent match\n"))

	hit, ok := sess.LastWatchHit()
	if !ok {
		t.Fatal("LastWatchHit() ok = false after a config-declared match")
	}
	if hit.Notify {
		t.Error("hit.Notify = true for a watch declared with notify: false")
	}
}

func TestSetConfigWatchersDropsInvalidPattern(t *testing.T) {
	m := newTestManager(t)
	sess := newTestSession(t, m, "watch-config-invalid")

	sess.setConfigWatchers([]WatchSpec{{Pattern: "[", Notify: true}, {Pattern: "ok", Notify: true}})

	if len(sess.watchers) != 1 {
		t.Fatalf("len(watchers) = %d, want 1 (the invalid pattern must be dropped, not the valid one)", len(sess.watchers))
	}
	if sess.watchers[0].pattern != "ok" {
		t.Errorf("watchers[0].pattern = %q, want %q", sess.watchers[0].pattern, "ok")
	}
}
