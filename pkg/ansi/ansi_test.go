package ansi

import (
	"bytes"
	"testing"
)

func TestWriterFiltersSequences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "texte simple", in: "hello\n", want: "hello\n"},
		{name: "SGR conservé", in: "\x1b[31mrouge\x1b[0m", want: "\x1b[31mrouge\x1b[0m"},
		{name: "SGR 256 conservé", in: "\x1b[38;5;2mvert\x1b[0m", want: "\x1b[38;5;2mvert\x1b[0m"},
		{name: "SGR truecolor conservé", in: "\x1b[38;2;1;2;3mrgb\x1b[0m", want: "\x1b[38;2;1;2;3mrgb\x1b[0m"},
		{name: "effacement de ligne conservé", in: "a\x1b[Kb", want: "a\x1b[Kb"},

		{name: "curseur haut supprimé", in: "a\x1b[Ab", want: "ab"},
		{name: "curseur positionné supprimé", in: "a\x1b[10;20Hb", want: "ab"},
		{name: "effacement écran supprimé", in: "a\x1b[2Jb", want: "ab"},
		{name: "curseur caché supprimé", in: "a\x1b[?25lb", want: "ab"},
		{name: "bracketed paste supprimé", in: "a\x1b[?2004hb", want: "ab"},
		{name: "alternate screen supprimé", in: "a\x1b[?1049hb", want: "ab"},
		{name: "SGR privé supprimé", in: "a\x1b[?1mb", want: "ab"},
		{name: "keypad supprimé", in: "a\x1b=b\x1b>c", want: "abc"},
		{name: "jeu de caractères supprimé", in: "a\x1b(Bb", want: "ab"},
		{name: "DCS supprimé", in: "a\x1bP1;2q...\x1b\\b", want: "ab"},

		{name: "OSC conservé (gocui le consomme)", in: "a\x1b]0;titre\x07b", want: "a\x1b]0;titre\x07b"},

		{name: "CR et tabulation intacts", in: "a\tb\r\nc", want: "a\tb\r\nc"},
		{name: "UTF-8 intact", in: "héllo → ✓", want: "héllo → ✓"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			n, err := NewWriter(&buf).Write([]byte(tt.in))
			if err != nil {
				t.Fatalf("Write: %v", err)
			}

			// Dropped bytes must still count as written, otherwise io.Copy
			// aborts with ErrShortWrite.
			if n != len(tt.in) {
				t.Errorf("Write = %d, want %d", n, len(tt.in))
			}

			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

// A pty read can cut a sequence anywhere; the filter is stateful and must not
// leak the fragments it has already consumed.
func TestWriterHandlesSplitSequences(t *testing.T) {
	const in = "a\x1b[?2004hb\x1b[31mrouge\x1b[0m\x1b[2Jc"
	const want = "ab\x1b[31mrouge\x1b[0mc"

	for split := 1; split < len(in); split++ {
		var buf bytes.Buffer
		w := NewWriter(&buf)

		if _, err := w.Write([]byte(in[:split])); err != nil {
			t.Fatalf("first write: %v", err)
		}
		if _, err := w.Write([]byte(in[split:])); err != nil {
			t.Fatalf("second write: %v", err)
		}

		if buf.String() != want {
			t.Errorf("split at %d: got %q, want %q", split, buf.String(), want)
		}
	}
}

// What a zsh prompt with a theme actually emits, as seen in the phase 1 spike.
func TestWriterOnRealPrompt(t *testing.T) {
	const in = "\x1b[?2004h\x1b[?25l\x1b[2;1H\x1b[J\x1b[38;5;2m~/workspace/lazyshell\x1b[0m \x1b[?25h"
	const want = "\x1b[38;5;2m~/workspace/lazyshell\x1b[0m "

	var buf bytes.Buffer
	if _, err := NewWriter(&buf).Write([]byte(in)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
