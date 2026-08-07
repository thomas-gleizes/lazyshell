package screen

import (
	"strings"
	"testing"
)

func TestPlainTailHasNoEscapeBytes(t *testing.T) {
	s := New(40, 5)
	write(t, s, "hello world\r\n")

	tail := s.PlainTail(5)

	if strings.ContainsRune(tail, '\x1b') {
		t.Fatalf("PlainTail contains an escape byte: %q", tail)
	}
}

func TestPlainTailContainsWrittenText(t *testing.T) {
	s := New(40, 5)
	write(t, s, "esc to interrupt\r\n")

	tail := s.PlainTail(5)
	if !strings.Contains(tail, "esc to interrupt") {
		t.Fatalf("PlainTail = %q, want it to contain the written line", tail)
	}
}

func TestPlainTailClampsToScreenHeight(t *testing.T) {
	s := New(40, 3)

	// n larger than the screen height must not panic or over-read; just
	// exercising the clamp path.
	_ = s.PlainTail(1000)
}

func TestPlainTailNonPositiveIsEmpty(t *testing.T) {
	s := New(40, 3)

	if got := s.PlainTail(0); got != "" {
		t.Fatalf("PlainTail(0) = %q, want empty", got)
	}

	if got := s.PlainTail(-1); got != "" {
		t.Fatalf("PlainTail(-1) = %q, want empty", got)
	}
}
