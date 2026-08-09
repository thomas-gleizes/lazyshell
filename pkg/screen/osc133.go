package screen

import "bytes"

// shellIntegration tracks OSC 133 shell-integration marks: the prompt/command
// boundaries zsh, fish and bash's standard integration hooks emit as
// OSC 133;A (prompt start), ;B (command/input start), ;C (output start) and
// ;D[;exit] (command finished).
//
// Positions are stored as monotonic ids, not scrollback indices: an index
// into Scrollback shifts meaning every time an old line is evicted, silently,
// which would make any mark stored that way wrong the moment a long-running
// session's scrollback fills up (see docs/adr/0008-integration-shell-osc-133.md).
// A monotonic id is Scrollback.Evicted() plus a position, taken at the moment
// the mark is observed — translating it back to a current absolute index
// (screenAbsoluteIndex/currentMonotonic below) later only has to subtract the
// evicted count at read time, and a negative result means the line the mark
// pointed to is gone.
type shellIntegration struct {
	// promptMarks holds every 'A' event's monotonic id, oldest first.
	promptMarks []int64

	// pendingB/pendingC are the monotonic ids of the current cycle's 'B'/'C'
	// events, valid only while haveB/haveC is set. A cycle runs A → [B] →
	// [C] → D; B or C (or both) can be missing, e.g. a command aborted before
	// producing output.
	pendingB, pendingC int64
	haveB, haveC       bool

	// lastOutputFrom/lastOutputTo bound the most recently finished command's
	// output, in monotonic ids, valid only while haveLastOutput is set.
	lastOutputFrom, lastOutputTo int64
	haveLastOutput               bool

	// lastExitCode/haveExitCode are the most recently finished command's exit
	// code, if the shell reported one — some shells omit it, e.g. on signal,
	// in which case the previous known code is left untouched rather than
	// cleared.
	lastExitCode int
	haveExitCode bool

	// lastExitSeq changes on every 'D', whether or not it carried a code, so
	// a caller polling on a tick can tell "a new command just finished" apart
	// from "the same command is still the most recent one".
	lastExitSeq int64

	// awaitingPromptAfterAlt is set when the alternate screen (vim, htop...)
	// relinquishes control, and cleared on the next 'A'. While set, B/C/D are
	// ignored: whatever the alternate-screen application printed on its way
	// out must never be mistaken for the shell's own output.
	awaitingPromptAfterAlt bool
}

// handleOsc133 parses an OSC 133 payload ("133;A", "133;C", "133;D;<code>",
// ...) and updates si accordingly. Registered as this Screen's OSC 133
// handler in NewWithScrollback, so it runs from inside term.Write with s.mu
// already held — see the field comment on Screen.
func (s *Screen) handleOsc133(data []byte) bool {
	parts := bytes.Split(data, []byte{';'})
	if len(parts) < 2 {
		return false
	}

	switch string(parts[1]) {
	case "A":
		s.si.promptMarks = append(s.si.promptMarks, s.currentMonotonic())
		s.si.haveB, s.si.haveC = false, false
		s.si.awaitingPromptAfterAlt = false
	case "B":
		if s.si.awaitingPromptAfterAlt {
			return true
		}
		s.si.pendingB, s.si.haveB = s.currentMonotonic(), true
	case "C":
		if s.si.awaitingPromptAfterAlt {
			return true
		}
		s.si.pendingC, s.si.haveC = s.currentMonotonic(), true
	case "D":
		if s.si.awaitingPromptAfterAlt {
			return true
		}
		s.commitCommand(parts)
	default:
		return false
	}

	return true
}

// commitCommand handles a 'D' event: it closes out the current cycle,
// recording the finished command's output range (if it produced one) and its
// exit code (if the shell reported one). Called with s.mu held.
func (s *Screen) commitCommand(parts [][]byte) {
	end := s.currentMonotonic()

	switch {
	case s.si.haveC:
		s.si.lastOutputFrom, s.si.lastOutputTo, s.si.haveLastOutput = s.si.pendingC, end, true
	case s.si.haveB:
		s.si.lastOutputFrom, s.si.lastOutputTo, s.si.haveLastOutput = s.si.pendingB, end, true
	}

	if len(parts) >= 3 {
		if code, ok := parseInt(parts[2]); ok {
			s.si.lastExitCode, s.si.haveExitCode = code, true
		}
	}

	s.si.lastExitSeq++
	s.si.haveB, s.si.haveC = false, false
}

// parseInt parses an unsigned-looking ASCII integer without pulling in
// strconv's full float/base-prefix handling for what is always a small,
// non-negative exit code.
func parseInt(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}

	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}

	return n, true
}

// setAltScreen is the AltScreen callback installed in NewWithScrollback,
// firing on every transition in either direction. It abandons any
// shell-integration cycle in progress — output from a full-screen
// application must never be attributed to whatever command was running when
// it took over — and arms awaitingPromptAfterAlt so B/C/D stay gated for the
// whole time the alternate screen owns the session (vim can print anything,
// including bytes that would otherwise look like shell-integration marks)
// and after it hands back control, until a real prompt (an 'A') is observed
// again. Called with s.mu held.
func (s *Screen) setAltScreen(bool) {
	s.si.haveB, s.si.haveC = false, false
	s.si.awaitingPromptAfterAlt = true
}

// currentMonotonic is the monotonic id of "right now": the position a new
// mark observed at this instant would occupy. Called with s.mu held.
func (s *Screen) currentMonotonic() int64 {
	sb := s.term.Scrollback()

	return int64(sb.Evicted()) + int64(sb.Len()) + int64(s.term.CursorPosition().Y)
}

// fromMonotonic translates a monotonic id into a current absolute index
// (Find's contract). ok is false once the line it pointed to has been
// evicted from the scrollback. Called with s.mu held.
func (s *Screen) fromMonotonic(id int64) (index int, ok bool) {
	idx := id - int64(s.term.Scrollback().Evicted())
	if idx < 0 {
		return 0, false
	}

	return int(idx), true
}

// PromptMarks returns the absolute index (Find's contract) of every prompt
// start still reachable in the scrollback, oldest first. A mark whose line
// has since been evicted is silently dropped.
func (s *Screen) PromptMarks() []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	marks := make([]int, 0, len(s.si.promptMarks))
	for _, id := range s.si.promptMarks {
		if idx, ok := s.fromMonotonic(id); ok {
			marks = append(marks, idx)
		}
	}

	return marks
}

// LastCommandOutputRange returns the [from, to] range (Find's contract) of
// the most recently finished command's output. ok is false when no command
// has completed yet, or when the start of its output has since scrolled out
// of the scrollback.
func (s *Screen) LastCommandOutputRange() (from, to int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.si.haveLastOutput {
		return 0, 0, false
	}

	from, ok = s.fromMonotonic(s.si.lastOutputFrom)
	if !ok {
		return 0, 0, false
	}

	to, _ = s.fromMonotonic(s.si.lastOutputTo)

	return from, to, true
}

// LastCommandExit returns the most recently finished command's exit code,
// whether the shell actually reported one, and seq — a value that changes
// once per finished command, letting a caller polling on a tick detect "a new
// command just finished" rather than re-reading a stale code. ok is false
// until the first command has finished.
func (s *Screen) LastCommandExit() (code int, hasCode bool, seq int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.si.lastExitSeq == 0 {
		return 0, false, 0, false
	}

	return s.si.lastExitCode, s.si.haveExitCode, s.si.lastExitSeq, true
}
