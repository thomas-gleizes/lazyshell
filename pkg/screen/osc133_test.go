package screen

import (
	"strings"
	"testing"
)

// A prompt boundary is what jump-to-prompt navigates between; PromptMarks
// must report it at the line it was actually written on.
func TestOsc133RecordsPromptMark(t *testing.T) {
	s := New(40, 10)

	if marks := s.PromptMarks(); len(marks) != 0 {
		t.Fatalf("PromptMarks() = %v before anything was written, want none", marks)
	}

	write(t, s, "\x1b]133;A\x07$ ")

	marks := s.PromptMarks()
	if len(marks) != 1 {
		t.Fatalf("PromptMarks() = %v, want exactly one mark", marks)
	}
}

// The full A → B → C → D;<code> cycle a shell-integration hook emits around
// one command, exercised end to end.
func TestOsc133FullCycleReportsExitCodeAndOutputRange(t *testing.T) {
	s := New(40, 10)

	if _, _, _, ok := s.LastCommandExit(); ok {
		t.Fatal("LastCommandExit() ok before any command finished")
	}
	if _, _, ok := s.LastCommandOutputRange(); ok {
		t.Fatal("LastCommandOutputRange() ok before any command finished")
	}

	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07false\r\n")
	write(t, s, "\x1b]133;C\x07")
	write(t, s, "command output\r\n")
	write(t, s, "\x1b]133;D;1\x07")

	code, hasCode, seq, ok := s.LastCommandExit()
	if !ok || !hasCode || code != 1 {
		t.Fatalf("LastCommandExit() = (%d, %v, %d, %v), want (1, true, _, true)", code, hasCode, seq, ok)
	}
	if seq == 0 {
		t.Error("lastExitSeq did not advance past its zero value")
	}

	from, to, ok := s.LastCommandOutputRange()
	if !ok {
		t.Fatal("LastCommandOutputRange() not ok after a command finished")
	}
	if from >= to {
		t.Errorf("LastCommandOutputRange() = (%d, %d), want from < to", from, to)
	}
}

// Some shells omit the exit code on D (e.g. after a signal); the previously
// known code must survive rather than being clobbered by a blank one.
func TestOsc133ExitCodeOmittedKeepsPreviousCode(t *testing.T) {
	s := New(40, 10)

	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07cmd1\r\n")
	write(t, s, "\x1b]133;D;1\x07")

	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07cmd2\r\n")
	write(t, s, "\x1b]133;D\x07") // no exit code this time

	code, hasCode, seq2, ok := s.LastCommandExit()
	if !ok || !hasCode || code != 1 {
		t.Fatalf("LastCommandExit() = (%d, %v, _, %v), want the previous code 1 to survive a codeless D", code, hasCode, ok)
	}
	if seq2 == 0 {
		t.Fatal("seq did not advance on the second command")
	}
}

// seq must change on every finished command, even when two consecutive runs
// happen to exit with the same code — it is the "something new happened"
// signal a caller polls on, not the code itself.
func TestOsc133ExitSeqAdvancesOnEachCommand(t *testing.T) {
	s := New(40, 10)

	write(t, s, "\x1b]133;A\x07$ \x1b]133;B\x07cmd1\r\n\x1b]133;D;0\x07")
	_, _, seq1, _ := s.LastCommandExit()

	write(t, s, "\x1b]133;A\x07$ \x1b]133;B\x07cmd2\r\n\x1b]133;D;0\x07")
	_, _, seq2, _ := s.LastCommandExit()

	if seq2 == seq1 {
		t.Errorf("seq stayed at %d across two separate commands", seq1)
	}
}

// Output range prefers C (output start) over B (input start): copying "the
// command's output" from B would include the command line itself.
func TestOsc133OutputRangeStartsAtC(t *testing.T) {
	s := New(40, 10)

	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07echo hi\r\n")
	write(t, s, "\x1b]133;C\x07")
	write(t, s, "hi\r\n")
	write(t, s, "\x1b]133;D;0\x07")

	from, _, ok := s.LastCommandOutputRange()
	if !ok {
		t.Fatal("LastCommandOutputRange() not ok")
	}

	// The line at `from` should be the one written right after C ("hi"), not
	// the one written right after B ("echo hi").
	marks := s.PromptMarks()
	if len(marks) != 1 {
		t.Fatalf("expected one prompt mark, got %v", marks)
	}
	if from <= marks[0] {
		t.Errorf("output range start %d is not after the prompt mark %d", from, marks[0])
	}
}

// A command that never produces a C (aborted before any output) still has a
// sensible range: it falls back to B.
func TestOsc133OutputRangeFallsBackToB(t *testing.T) {
	s := New(40, 10)

	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07sleep 1\r\n")
	write(t, s, "\x1b]133;D;130\x07") // Ctrl-C, no C ever arrived

	from, to, ok := s.LastCommandOutputRange()
	if !ok {
		t.Fatal("LastCommandOutputRange() not ok with a B-only cycle")
	}
	if from > to {
		t.Errorf("LastCommandOutputRange() = (%d, %d), want from <= to", from, to)
	}
}

// A full-screen application (vim, htop) can print arbitrary bytes, including
// sequences that would otherwise look like shell-integration marks. Entering
// the alternate screen must abandon whatever cycle was in progress, and
// B/C/D must stay gated until a real prompt is seen again on return.
func TestOsc133AltScreenGatesUntilNextPrompt(t *testing.T) {
	s := New(40, 10)

	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07vim\r\n")

	// vim takes the alternate screen mid-command.
	write(t, s, "\x1b[?1049h")

	// Whatever vim prints must not be trusted as this command's C/D.
	write(t, s, "\x1b]133;C\x07\x1b]133;D;0\x07")

	if _, _, _, ok := s.LastCommandExit(); ok {
		t.Fatal("a D received while awaiting a prompt after alt-screen was accepted")
	}

	// vim exits, back to the main screen — still gated until the shell
	// actually redraws its prompt.
	write(t, s, "\x1b[?1049l")
	write(t, s, "\x1b]133;D;0\x07")

	if _, _, _, ok := s.LastCommandExit(); ok {
		t.Fatal("a D received before the next prompt mark was accepted")
	}

	// The shell redraws its prompt: the gate lifts.
	write(t, s, "\x1b]133;A\x07$ ")
	write(t, s, "\x1b]133;B\x07echo hi\r\nhi\r\n\x1b]133;D;0\x07")

	if _, _, _, ok := s.LastCommandExit(); !ok {
		t.Error("a D after the next prompt mark should have been accepted")
	}
}

// The acceptance criterion for this feature: a prompt mark must keep
// pointing at the right line even after enough later output has pushed it
// past the scrollback's capacity, not silently drift or vanish.
//
// A single-row terminal makes every "\r\n" push exactly one line to
// scrollback, which is what lets this test predict eviction counts exactly.
// With a 5-line scrollback: 3 filler lines land first (no eviction yet, len
// 3), then the marker (len 4), then 4 more filler lines exactly fill the
// buffer to its 5-line capacity and evict the first 3 filler lines one at a
// time — landing right before the mark's own line would be the next one
// evicted, so this only proves survival, not the drop covered by
// TestPromptMarkDroppedOnceEvicted below.
func TestPromptMarkSurvivesScrollbackTruncation(t *testing.T) {
	s := NewWithScrollback(40, 1, 5)

	write(t, s, "f1\r\nf2\r\nf3\r\n")
	write(t, s, "\x1b]133;A\x07marker-line\r\n")

	marks := s.PromptMarks()
	if len(marks) != 1 {
		t.Fatalf("PromptMarks() = %v, want exactly one mark", marks)
	}
	before := marks[0]

	write(t, s, "g1\r\ng2\r\ng3\r\ng4\r\n")

	after := s.PromptMarks()
	if len(after) != 1 {
		t.Fatalf("PromptMarks() = %v after more output, want the mark to still be there", after)
	}

	// The absolute index must have shifted down by exactly the number of
	// lines evicted so far (3: f1, f2, f3), matching Find's contract for
	// every other consumer of this window.
	if want := before - 3; after[0] != want {
		t.Errorf("PromptMarks()[0] = %d after eviction, want %d (shifted down by 3)", after[0], want)
	}

	// Confirm the shifted index still resolves to the right content via
	// RenderAt, exactly the way jump-to-prompt would use it: offset that
	// puts the mark at the top of the window.
	offset := s.ScrollbackLen() - after[0]
	out := s.RenderAt(offset, "")
	if !strings.Contains(out, "marker-line") {
		t.Errorf("RenderAt(%d) does not show the marker line after truncation:\n%s", offset, out)
	}
}

// The other side of truncation-survival: once a mark's own line has been
// evicted entirely, it must be dropped rather than resurrected at the wrong
// place.
func TestPromptMarkDroppedOnceEvicted(t *testing.T) {
	s := NewWithScrollback(40, 5, 10)

	write(t, s, "\x1b]133;A\x07$ old-marker\r\n")

	if len(s.PromptMarks()) != 1 {
		t.Fatal("expected the mark to be recorded")
	}

	// Push enough plain output to evict every line the mark could have
	// pointed to, several times over the scrollback's capacity.
	for i := 0; i < 40; i++ {
		write(t, s, "filler\r\n")
	}

	if marks := s.PromptMarks(); len(marks) != 0 {
		t.Errorf("PromptMarks() = %v after the mark's line was evicted, want none", marks)
	}
}
