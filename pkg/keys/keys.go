// Package keys translates gocui key events into the byte sequences a terminal
// would have sent to a pty.
//
// gocui hands us decoded events (a Key constant plus an optional rune), not raw
// input bytes: the terminal driver has already parsed the escape sequences on
// the way in. To drive a shell behind a pty we have to encode them back. This
// package is the whole of that translation, kept out of pkg/gui so it can be
// unit-tested without a terminal.
package keys

import (
	"unicode/utf8"

	"github.com/jesseduffield/gocui"
)

const esc = 0x1b

// specialKeys maps the keys gocui reports as dedicated constants (values >=
// 256, i.e. anything the terminal sends as an escape sequence) back to their
// xterm encoding, in normal cursor mode.
var specialKeys = map[gocui.Key]string{
	gocui.KeyArrowUp:    "\x1b[A",
	gocui.KeyArrowDown:  "\x1b[B",
	gocui.KeyArrowRight: "\x1b[C",
	gocui.KeyArrowLeft:  "\x1b[D",

	// gocui has no dedicated constants for these: it reuses tcell's unused
	// F62/F63, which are *the same values* as MouseRight/MouseLeft. The
	// ambiguity is only safe because lazyshell keeps the mouse disabled
	// (g.Mouse = false); enabling it later means dropping these two entries.
	gocui.KeyShiftArrowUp:   "\x1b[1;2A",
	gocui.KeyShiftArrowDown: "\x1b[1;2B",

	gocui.KeyHome: "\x1b[H",
	gocui.KeyEnd:  "\x1b[F",

	gocui.KeyInsert: "\x1b[2~",
	gocui.KeyDelete: "\x1b[3~",
	gocui.KeyPgup:   "\x1b[5~",
	gocui.KeyPgdn:   "\x1b[6~",

	gocui.KeyBacktab: "\x1b[Z",

	gocui.KeyF1:  "\x1bOP",
	gocui.KeyF2:  "\x1bOQ",
	gocui.KeyF3:  "\x1bOR",
	gocui.KeyF4:  "\x1bOS",
	gocui.KeyF5:  "\x1b[15~",
	gocui.KeyF6:  "\x1b[17~",
	gocui.KeyF7:  "\x1b[18~",
	gocui.KeyF8:  "\x1b[19~",
	gocui.KeyF9:  "\x1b[20~",
	gocui.KeyF10: "\x1b[21~",
	gocui.KeyF11: "\x1b[23~",
	gocui.KeyF12: "\x1b[24~",
}

// Translate encodes a gocui key event as the bytes to write to a pty. It
// returns nil when the event has no meaningful encoding (mouse events, unknown
// keys), in which case the caller should drop it.
//
// ModAlt is encoded the way terminals do it: an ESC prefix before the sequence.
func Translate(key gocui.Key, ch rune, mod gocui.Modifier) []byte {
	out := encode(key, ch)
	if out == nil {
		return nil
	}

	if mod&gocui.ModAlt != 0 {
		return append([]byte{esc}, out...)
	}

	return out
}

func encode(key gocui.Key, ch rune) []byte {
	// A printable character: gocui clears Key and reports the rune.
	if ch != 0 {
		buf := make([]byte, utf8.RuneLen(ch))
		utf8.EncodeRune(buf, ch)

		return buf
	}

	if seq, ok := specialKeys[key]; ok {
		return []byte(seq)
	}

	switch {
	// Keys gocui already reports as their control byte: Enter (CR, 13),
	// Tab (9), Esc (27), Backspace (8)...
	case key < 32:
		return []byte{byte(key)}

	// ...spacebar, which gocui reports as key 32 with no rune.
	case key == gocui.KeySpace:
		return []byte{' '}

	// Ctrl-<x>: gocui reports 64+<letter>, i.e. the ASCII code of the
	// uppercase letter, not the control byte. Ctrl-Space is 64 (NUL) and
	// Ctrl-_ is 95, so the whole [64,95] range is a control combination.
	case key >= 64 && key <= 95:
		return []byte{byte(key) & 0x1f}

	case key == gocui.KeyBackspace2:
		return []byte{0x7f}
	}

	// Mouse events and anything we do not know how to encode.
	return nil
}
