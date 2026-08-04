package main

import (
	"os"
	"testing"

	"github.com/jesseduffield/gocui"
)

// newEditSpike builds a spike whose pty end is a pipe, so the bytes the editor
// forwards can be read back. No gocui main loop is involved: the editor is a
// pure function of the event it receives.
func newEditSpike(t *testing.T) (*spike, *os.File) {
	t.Helper()

	shellEnd, spikeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	// A headless gui, without a main loop: quit() only needs somewhere to
	// enqueue its update.
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode: gocui.OutputTrue,
		Headless:   true,
		Width:      80,
		Height:     24,
	})
	if err != nil {
		t.Fatalf("NewGui: %v", err)
	}

	t.Cleanup(func() {
		g.Close()
		_ = spikeEnd.Close()
		_ = shellEnd.Close()
	})

	return &spike{g: g, ptmx: spikeEnd, prefixKey: defaultPrefixKey}, shellEnd
}

func read(t *testing.T, f *os.File, n int) string {
	t.Helper()

	buf := make([]byte, n)

	got, err := f.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	return string(buf[:got])
}

// The event shape below is what gocui actually delivers for Ctrl-B: tcell
// encodes control keys as 64+letter (KeyCtrlB = 66) and gocui clears the
// Ctrl modifier on the way through. Asserting on the raw byte 0x02 would test
// nothing, since that is not what reaches the editor.
func TestEditPrefixIsSwallowedThenQuits(t *testing.T) {
	s, _ := newEditSpike(t)

	if !s.edit(nil, gocui.KeyCtrlB, 0, gocui.ModNone) {
		t.Fatal("the editor must consume the prefix")
	}

	if !s.prefixPending {
		t.Fatal("prefix not armed")
	}

	s.edit(nil, 0, 'q', gocui.ModNone)

	if s.prefixPending {
		t.Error("prefix still armed after the qualifying key")
	}

	if !s.quitRequested {
		t.Error("prefix + q did not request a quit")
	}
}

func TestEditDoubledPrefixIsForwarded(t *testing.T) {
	s, shell := newEditSpike(t)

	s.edit(nil, gocui.KeyCtrlB, 0, gocui.ModNone)
	s.edit(nil, gocui.KeyCtrlB, 0, gocui.ModNone)

	if got := read(t, shell, 1); got != "\x02" {
		t.Errorf("shell received %q, want %q", got, "\x02")
	}

	if s.quitRequested {
		t.Error("a doubled prefix must not quit")
	}
}

// After the prefix, a key that means nothing is swallowed and disarms it.
func TestEditUnknownKeyAfterPrefixIsSwallowed(t *testing.T) {
	s, _ := newEditSpike(t)

	s.edit(nil, gocui.KeyCtrlB, 0, gocui.ModNone)
	s.edit(nil, 0, 'z', gocui.ModNone)

	if s.prefixPending {
		t.Error("prefix still armed")
	}

	if s.quitRequested {
		t.Error("prefix + z must not quit")
	}
}

func TestEditForwardsEverythingElse(t *testing.T) {
	tests := []struct {
		name string
		key  gocui.Key
		ch   rune
		want string
	}{
		{name: "lettre", ch: 'a', want: "a"},
		{name: "q seul va au shell", ch: 'q', want: "q"},
		{name: "entrée", key: gocui.KeyEnter, want: "\r"},
		{name: "ctrl-c", key: gocui.KeyCtrlC, want: "\x03"},
		{name: "flèche haut", key: gocui.KeyArrowUp, want: "\x1b[A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, shell := newEditSpike(t)

			if !s.edit(nil, tt.key, tt.ch, gocui.ModNone) {
				t.Fatal("the editor must consume every event")
			}

			if got := read(t, shell, len(tt.want)); got != tt.want {
				t.Errorf("shell received %q, want %q", got, tt.want)
			}

			if s.quitRequested {
				t.Error("unexpected quit")
			}
		})
	}
}

// LAZYSHELL_PREFIX exists so a user whose terminal or multiplexer eats Ctrl-B
// can pick another key without touching the code.
func TestPrefixFromEnv(t *testing.T) {
	tests := []struct {
		value string
		want  gocui.Key
	}{
		{value: "", want: defaultPrefixKey},
		{value: "Ctrl+A", want: gocui.KeyCtrlA},
		{value: "Ctrl+G", want: gocui.KeyCtrlG},
		{value: "CtrlA", want: defaultPrefixKey}, // gocui.Parse veut la forme "Ctrl+A"
		{value: "n'importe quoi", want: defaultPrefixKey},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("LAZYSHELL_PREFIX", tt.value)

			if got := prefixFromEnv(); got != tt.want {
				t.Errorf("prefixFromEnv() = %d, want %d", got, tt.want)
			}
		})
	}
}

// A custom prefix must actually be the key that arms the sequence, and Ctrl-B
// must then go to the shell like any other key.
func TestCustomPrefixIsHonoured(t *testing.T) {
	s, shell := newEditSpike(t)
	s.prefixKey = gocui.KeyCtrlA

	s.edit(nil, gocui.KeyCtrlB, 0, gocui.ModNone)

	if s.prefixPending {
		t.Error("Ctrl-B armed the prefix although Ctrl-A is configured")
	}

	if got := read(t, shell, 1); got != "\x02" {
		t.Errorf("shell received %q, want %q", got, "\x02")
	}

	s.edit(nil, gocui.KeyCtrlA, 0, gocui.ModNone)

	if !s.prefixPending {
		t.Error("Ctrl-A did not arm the prefix")
	}
}
