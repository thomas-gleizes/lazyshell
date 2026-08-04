package keys

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/jesseduffield/gocui"
)

func TestTranslate(t *testing.T) {
	tests := []struct {
		name string
		key  gocui.Key
		ch   rune
		mod  gocui.Modifier
		want string
	}{
		{name: "lettre", ch: 'a', want: "a"},
		{name: "rune multi-octets", ch: 'é', want: "é"},
		{name: "espace", key: gocui.KeySpace, want: " "},

		{name: "entrée envoie CR", key: gocui.KeyEnter, want: "\r"},
		{name: "tabulation", key: gocui.KeyTab, want: "\t"},
		{name: "échap", key: gocui.KeyEsc, want: "\x1b"},
		{name: "backspace", key: gocui.KeyBackspace, want: "\x08"},
		{name: "backspace2 envoie DEL", key: gocui.KeyBackspace2, want: "\x7f"},

		{name: "ctrl-c", key: gocui.KeyCtrlC, want: "\x03"},
		{name: "ctrl-a", key: gocui.KeyCtrlA, want: "\x01"},
		{name: "ctrl-z", key: gocui.KeyCtrlZ, want: "\x1a"},
		{name: "ctrl-espace envoie NUL", key: gocui.KeyCtrlSpace, want: "\x00"},
		{name: "ctrl-underscore", key: gocui.KeyCtrlUnderscore, want: "\x1f"},

		{name: "flèche haut", key: gocui.KeyArrowUp, want: "\x1b[A"},
		{name: "flèche bas", key: gocui.KeyArrowDown, want: "\x1b[B"},
		{name: "flèche droite", key: gocui.KeyArrowRight, want: "\x1b[C"},
		{name: "flèche gauche", key: gocui.KeyArrowLeft, want: "\x1b[D"},
		{name: "home", key: gocui.KeyHome, want: "\x1b[H"},
		{name: "fin", key: gocui.KeyEnd, want: "\x1b[F"},
		{name: "suppr", key: gocui.KeyDelete, want: "\x1b[3~"},
		{name: "page haut", key: gocui.KeyPgup, want: "\x1b[5~"},
		{name: "F1", key: gocui.KeyF1, want: "\x1bOP"},
		{name: "F5", key: gocui.KeyF5, want: "\x1b[15~"},

		{name: "alt+lettre préfixe ESC", ch: 'b', mod: gocui.ModAlt, want: "\x1bb"},
		{name: "alt+flèche préfixe ESC", key: gocui.KeyArrowLeft, mod: gocui.ModAlt, want: "\x1b\x1b[D"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Translate(tt.key, tt.ch, tt.mod)
			if string(got) != tt.want {
				t.Errorf("Translate(%d, %q, %d) = %q, want %q", tt.key, tt.ch, tt.mod, got, tt.want)
			}
		})
	}
}

// Mouse events reach the editor like any other event; they must not be
// forwarded to the shell as garbage bytes.
func TestTranslateDropsMouseEvents(t *testing.T) {
	for _, key := range []gocui.Key{
		gocui.MouseWheelUp,
		gocui.MouseWheelDown,
		gocui.MouseRelease,
		gocui.MouseMiddle,
	} {
		if got := Translate(key, 0, gocui.ModNone); got != nil {
			t.Errorf("Translate(%d) = %q, want nil", key, got)
		}
	}
}

// gocui reuses tcell's spare F62/F63 for both Shift-arrows and the left/right
// mouse buttons, so the two are indistinguishable. This test exists to make
// that collision fail loudly if a future gocui bump changes it — and to record
// why the mouse must stay disabled.
func TestShiftArrowsCollideWithMouseButtons(t *testing.T) {
	if gocui.KeyShiftArrowUp != gocui.MouseRight || gocui.KeyShiftArrowDown != gocui.MouseLeft {
		t.Fatal("gocui no longer aliases Shift-arrows onto the mouse buttons: " +
			"the mouse can be enabled without losing Shift-arrow support")
	}
}

// Every Ctrl-<letter> must land on its control byte: this is the range where
// gocui's encoding (ASCII code of the letter) differs from what a terminal
// sends, and where a mistake is silent.
func TestTranslateAllControlLetters(t *testing.T) {
	ctrlKeys := []gocui.Key{
		gocui.KeyCtrlA, gocui.KeyCtrlB, gocui.KeyCtrlC, gocui.KeyCtrlD,
		gocui.KeyCtrlE, gocui.KeyCtrlF, gocui.KeyCtrlG, gocui.KeyCtrlJ,
		gocui.KeyCtrlK, gocui.KeyCtrlL, gocui.KeyCtrlN, gocui.KeyCtrlO,
		gocui.KeyCtrlP, gocui.KeyCtrlQ, gocui.KeyCtrlR, gocui.KeyCtrlS,
		gocui.KeyCtrlT, gocui.KeyCtrlU, gocui.KeyCtrlV, gocui.KeyCtrlW,
		gocui.KeyCtrlX, gocui.KeyCtrlY, gocui.KeyCtrlZ,
	}

	for _, key := range ctrlKeys {
		got := Translate(key, 0, gocui.ModNone)
		if len(got) != 1 {
			t.Fatalf("Translate(%d) = %q, want a single byte", key, got)
		}

		want := byte(key) - 64
		if got[0] != want {
			t.Errorf("Translate(%d) = %#x, want %#x", key, got[0], want)
		}
	}
}

// The same Ctrl-B keypress arrives in different shapes depending on the
// terminal's keyboard protocol. All of them must fold onto KeyCtrlB, otherwise
// the escape prefix is indistinguishable from typing a plain letter.
func TestNormalizeCtrlShapes(t *testing.T) {
	const modCtrl = gocui.Modifier(tcell.ModCtrl)

	tests := []struct {
		name string
		key  gocui.Key
		ch   rune
		mod  gocui.Modifier
	}{
		{name: "forme gocui", key: gocui.KeyCtrlB},
		{name: "rune minuscule + ModCtrl", ch: 'b', mod: modCtrl},
		{name: "rune majuscule + ModCtrl", ch: 'B', mod: modCtrl},
		{name: "ModCtrl combiné", ch: 'b', mod: modCtrl | gocui.ModAlt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ch, mod := Normalize(tt.key, tt.ch, tt.mod)

			if key != gocui.KeyCtrlB {
				t.Errorf("key = %d, want %d", key, gocui.KeyCtrlB)
			}

			if ch != 0 {
				t.Errorf("ch = %q, want 0", ch)
			}

			if mod&modCtrl != 0 {
				t.Errorf("mod = %d, ModCtrl should have been consumed", mod)
			}
		})
	}
}

// A plain letter must not be mistaken for a control combination.
func TestNormalizeLeavesPlainRunesAlone(t *testing.T) {
	key, ch, mod := Normalize(0, 'b', gocui.ModNone)

	if key != 0 || ch != 'b' || mod != gocui.ModNone {
		t.Errorf("Normalize(0, 'b', 0) = (%d, %q, %d), want (0, 'b', 0)", key, ch, mod)
	}
}

// Ctrl-B in any shape must still encode to the same byte.
func TestTranslateCtrlShapes(t *testing.T) {
	const modCtrl = gocui.Modifier(tcell.ModCtrl)

	for _, tt := range []struct {
		name string
		key  gocui.Key
		ch   rune
		mod  gocui.Modifier
	}{
		{name: "forme gocui", key: gocui.KeyCtrlB},
		{name: "rune + ModCtrl", ch: 'b', mod: modCtrl},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Translate(tt.key, tt.ch, tt.mod); string(got) != "\x02" {
				t.Errorf("Translate = %q, want %q", got, "\x02")
			}
		})
	}
}
