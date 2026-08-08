package gui

import (
	"testing"
	"time"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// A pid change means a different process, not a continuation: keeping the old
// points would draw `vim`'s memory as a cliff off the end of `ls`'s.
func TestPerfTrackResetsWhenThePidChanges(t *testing.T) {
	var track perfTrack

	track.push(1, 10, 100)
	track.push(1, 20, 200)

	if len(track.cpu) != 2 {
		t.Fatalf("len(cpu) = %d after two pushes for the same pid, want 2", len(track.cpu))
	}

	track.push(2, 5, 50)

	if len(track.cpu) != 1 || track.cpu[0] != 5 {
		t.Errorf("cpu = %v after a pid change, want just the new process's point", track.cpu)
	}

	if track.pid != 2 {
		t.Errorf("pid = %d, want 2", track.pid)
	}
}

func TestAppendCappedKeepsTheNewestAndStaysBounded(t *testing.T) {
	var series []float64

	for i := range perfHistoryLen + 50 {
		series = appendCapped(series, float64(i))
	}

	if len(series) != perfHistoryLen {
		t.Fatalf("len = %d, want it capped at %d", len(series), perfHistoryLen)
	}

	// The newest sample must be the last one, and the oldest ones gone.
	if got, want := series[len(series)-1], float64(perfHistoryLen+49); got != want {
		t.Errorf("last = %v, want the newest %v", got, want)
	}

	if got, want := series[0], float64(50); got != want {
		t.Errorf("first = %v, want the oldest surviving %v", got, want)
	}
}

// The regression this exists for: after a foreground command finished, the big
// chart kept plotting its flat zero line while the shell was visibly busy.
func TestChartTrackFallsBackToTheShellWhenTheForegroundIsGone(t *testing.T) {
	var history perfHistory

	history.track(false).push(1, 50, 100)
	history.track(true).push(2, 0, 10)

	if history.chartTrack() != &history.foreground {
		t.Fatal("chartTrack did not prefer the foreground while it had points")
	}

	history.dropForeground()

	if history.chartTrack() != &history.shell {
		t.Error("chartTrack still points at a foreground process that is gone")
	}
}

func TestForgetPerfHistoryDropsTheSeries(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.perfHistories = map[string]*perfHistory{"a": {version: 3}}

	gui.forgetPerfHistory("a")

	if _, ok := gui.perfSnapshotFor("a"); ok {
		t.Error("the series survived forgetPerfHistory, so a reused id would inherit them")
	}
}

// The renderer reads a copy, not the live series: appendCapped rewrites its
// backing array in place, so a renderer holding the original would watch it
// change mid-draw from the sampler's goroutine.
func TestPerfSnapshotIsACopy(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	history := &perfHistory{}
	history.track(false).push(1, 10, 100)
	history.track(false).push(1, 20, 200)
	gui.perfHistories = map[string]*perfHistory{"a": history}

	snap, ok := gui.perfSnapshotFor("a")
	if !ok {
		t.Fatal("perfSnapshotFor found nothing")
	}

	if len(snap.shellCPU) != 2 {
		t.Fatalf("len(shellCPU) = %d, want 2", len(snap.shellCPU))
	}

	history.track(false).push(1, 99, 999)

	if snap.shellCPU[len(snap.shellCPU)-1] == 99 {
		t.Error("the snapshot shares its backing array with the live series")
	}
}

// The version is what lets the renderer skip rebuilding its text; a sample that
// did not bump it would freeze the panel.
func TestApplyBumpsTheVersionAndRecordsTheSample(t *testing.T) {
	// A bare Session, not a real one: apply only reads its name and start
	// time, and spawning a shell here would drag in the pre-existing teardown
	// race that `-race` trips on (see the concurrency test below).
	sess := &session.Session{ID: "perf", CreatedAt: time.Now()}

	var history perfHistory

	history.apply(sess, []session.ProcStats{{PID: 1, Comm: "sh", CPUTime: time.Second, SampledAt: time.Now()}})

	if history.version != 1 {
		t.Errorf("version = %d after one sample, want 1", history.version)
	}

	if len(history.latest) != 1 {
		t.Errorf("len(latest) = %d, want the sample kept for the figures", len(history.latest))
	}

	if len(history.shell.cpu) != 1 {
		t.Errorf("len(shell.cpu) = %d, want the point appended", len(history.shell.cpu))
	}

	// An empty sample is not an error worth clearing the curves over — only the
	// figures go away.
	history.apply(sess, nil)

	if history.version != 2 {
		t.Errorf("version = %d, want it bumped even for an empty sample", history.version)
	}

	if history.latest != nil {
		t.Error("latest survived an empty sample")
	}

	if len(history.shell.cpu) != 1 {
		t.Errorf("len(shell.cpu) = %d, want the history kept", len(history.shell.cpu))
	}

	if history.err == nil {
		t.Error("err is nil after an empty sample, want the panel told why")
	}
}

// End to end over the real batching path: every live session gets a history,
// and dead ones are pruned rather than kept for the life of the process.
func TestSamplePerfCoversEverySessionAndPrunesTheRest(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.perfIntervalMs = 5000

	first, err := gui.sessions.New("one", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	second, err := gui.sessions.New("two", "/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.samplePerf(); err != nil {
		t.Fatalf("samplePerf: %v", err)
	}

	for _, id := range []string{first.ID, second.ID} {
		snap, ok := gui.perfSnapshotFor(id)
		if !ok {
			t.Fatalf("session %s has no history after sampling", id)
		}

		if len(snap.latest) == 0 {
			t.Errorf("session %s was sampled but has no figures", id)
		}
	}

	// A stale entry from a session that no longer exists must not survive.
	gui.mu.Lock()
	gui.perfHistories["ghost"] = &perfHistory{}
	gui.mu.Unlock()

	if err := gui.samplePerf(); err != nil {
		t.Fatalf("samplePerf: %v", err)
	}

	if _, ok := gui.perfSnapshotFor("ghost"); ok {
		t.Error("a history for a session that no longer exists survived sampling")
	}
}

// Sampling spawns a process, so someone who never opens the tab must be able to
// stop paying for it entirely.
func TestSamplePerfIsOffAtIntervalZero(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.perfIntervalMs = 0

	if _, err := gui.sessions.New("one", "/bin/sh"); err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := gui.samplePerf(); err != nil {
		t.Fatalf("samplePerf: %v", err)
	}

	if len(gui.perfHistories) != 0 {
		t.Errorf("sampling happened with perf.refresh_interval_ms = 0: %v", gui.perfHistories)
	}
}

// The whole point of the version/snapshot design is that two goroutines now
// touch this state: samplePerf writes it on goEvery's, the render task reads it
// on its own. Exercised here without real sessions on purpose — `go test -race`
// over the whole package trips on a pre-existing race in session teardown
// (pkg/screen's Emulator.Close against its own Read), which would mask this.
func TestPerfHistoryIsSafeForConcurrentSampleAndRender(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	sess := &session.Session{ID: "s1", CreatedAt: time.Now()}
	history := &perfHistory{}
	gui.perfHistories = map[string]*perfHistory{"s1": history}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range 200 {
			gui.mu.Lock()
			history.apply(sess, []session.ProcStats{{
				PID:       1,
				Comm:      "sh",
				CPUTime:   time.Duration(i) * time.Millisecond,
				SampledAt: time.Now(),
			}})
			gui.mu.Unlock()
		}
	}()

	for range 200 {
		snap, ok := gui.perfSnapshotFor("s1")
		if !ok {
			continue
		}

		// Read the copy the way the renderer would, so a shared backing array
		// would show up as a race rather than silently working.
		_ = maxOf(snap.shellCPU)
		_ = len(snap.latest)
	}

	<-done
}

func TestMinMaxOf(t *testing.T) {
	tests := []struct {
		name     string
		series   []float64
		min, max float64
	}{
		{name: "empty", series: nil, min: 0, max: 0},
		{name: "single", series: []float64{7}, min: 7, max: 7},
		{name: "several", series: []float64{3, 9, 1, 4}, min: 1, max: 9},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := minOf(tc.series); got != tc.min {
				t.Errorf("minOf = %v, want %v", got, tc.min)
			}

			if got := maxOf(tc.series); got != tc.max {
				t.Errorf("maxOf = %v, want %v", got, tc.max)
			}
		})
	}
}
