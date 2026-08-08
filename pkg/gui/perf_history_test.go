package gui

import "testing"

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

func TestPerfHistoryForIsPerSessionAndStable(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	first := gui.perfHistoryFor("a")
	if first == nil {
		t.Fatal("perfHistoryFor returned nil")
	}

	// Stable across calls, or every render task would start from scratch —
	// which is the whole reason this lives on Gui rather than in the closure.
	if again := gui.perfHistoryFor("a"); again != first {
		t.Error("perfHistoryFor returned a different history for the same session")
	}

	if other := gui.perfHistoryFor("b"); other == first {
		t.Error("two sessions share one history")
	}
}

func TestForgetPerfHistoryDropsTheSeries(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	first := gui.perfHistoryFor("a")
	gui.forgetPerfHistory("a")

	if again := gui.perfHistoryFor("a"); again == first {
		t.Error("the series survived forgetPerfHistory, so a reused id would inherit them")
	}
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
