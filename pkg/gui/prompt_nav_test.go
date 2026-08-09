package gui

import "testing"

// jumpToPrompt moves to the nearest prompt boundary before/after the current
// top of the window, and clamps at the oldest/newest mark rather than
// wrapping — "the next prompt" past the newest one has no sensible target.
func TestJumpToPromptMovesBetweenMarksAndClampsAtTheEnds(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	sess := gui.selectedSession()
	waitForSessionScreen(t, gui, "$")

	scr := sess.Screen()
	writeLines := func(n int) {
		t.Helper()

		for i := 0; i < n; i++ {
			if _, err := scr.Write([]byte("filler\r\n")); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}

	if _, err := scr.Write([]byte("\x1b]133;A\x07$ mark-one\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Enough filler to push mark-one well into scrollback before mark-two is
	// even written — the 80x24 headless terminal's output panel is far
	// fewer than 60 rows tall.
	writeLines(60)
	if _, err := scr.Write([]byte("\x1b]133;A\x07$ mark-two\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// And again after mark-two, so it too is off the live screen by the time
	// jumpToPrompt starts from offset 0 — otherwise "the top of the window"
	// at the live bottom would not actually be past it yet.
	writeLines(60)

	if len(scr.PromptMarks()) != 2 {
		t.Fatalf("PromptMarks() = %v, want two marks", scr.PromptMarks())
	}

	if gui.getScrollOffset() != 0 {
		t.Fatalf("scroll offset = %d before any jump, want 0 (live bottom)", gui.getScrollOffset())
	}

	// From the live bottom, jumping backward lands on the most recent mark
	// first (mark-two), then the one before it (mark-one).
	gui.jumpToPrompt(-1)
	afterFirstJump := gui.getScrollOffset()
	if afterFirstJump == 0 {
		t.Fatal("jumpToPrompt(-1) did not move the scroll offset")
	}

	gui.jumpToPrompt(-1)
	afterSecondJump := gui.getScrollOffset()
	if afterSecondJump <= afterFirstJump {
		t.Fatalf("jumpToPrompt(-1) a second time did not move further back (%d -> %d)", afterFirstJump, afterSecondJump)
	}

	// No earlier mark than mark-one: clamp, not wrap.
	gui.jumpToPrompt(-1)
	if gui.getScrollOffset() != afterSecondJump {
		t.Errorf("jumpToPrompt(-1) past the oldest mark moved the offset (%d -> %d), want it to clamp", afterSecondJump, gui.getScrollOffset())
	}

	// Jumping forward retraces to mark-two.
	gui.jumpToPrompt(1)
	if gui.getScrollOffset() != afterFirstJump {
		t.Errorf("jumpToPrompt(1) = offset %d, want back at mark-two's offset %d", gui.getScrollOffset(), afterFirstJump)
	}

	// No later mark than mark-two: clamp, not wrap back to the live bottom.
	gui.jumpToPrompt(1)
	if gui.getScrollOffset() != afterFirstJump {
		t.Errorf("jumpToPrompt(1) past the newest mark moved the offset (%d -> %d), want it to clamp", afterFirstJump, gui.getScrollOffset())
	}
}

// With no prompt marks recorded at all, jumping in either direction is a
// no-op rather than a crash.
func TestJumpToPromptNoOpWithNoMarks(t *testing.T) {
	gui, _ := newOutputTestGui(t)
	waitForSessionScreen(t, gui, "$")

	gui.jumpToPrompt(-1)
	if gui.getScrollOffset() != 0 {
		t.Errorf("scroll offset = %d after jumping with no marks, want 0", gui.getScrollOffset())
	}

	gui.jumpToPrompt(1)
	if gui.getScrollOffset() != 0 {
		t.Errorf("scroll offset = %d after jumping with no marks, want 0", gui.getScrollOffset())
	}
}
