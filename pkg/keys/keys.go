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
	"unicode"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/jesseduffield/gocui"
)

// modCtrl is tcell's control modifier seen through gocui's alias. gocui
// normally clears it before handing the event over, but only when it is the
// *only* modifier set: a terminal reporting Ctrl together with anything else
// leaves it in place.
const modCtrl = gocui.Modifier(tcell.ModCtrl)

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
	// F62/F63, which are *the same values* as MouseRight/MouseLeft. These two
	// entries are therefore only reachable while the mouse is off; with
	// mouse.enabled set, pkg/gui's editOutput drops mouse-valued keys before
	// they ever get here, and the ambiguity is resolved in the mouse's favour.
	// See docs/adr/0003-souris.md.
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

// applicationCursorKeys overrides specialKeys while DECCKM (ESC[?1h) is set:
// the cursor and Home/End keys switch from the CSI form to the SS3 one. vim,
// less and a good part of the readline/ncurses world set this mode on entry,
// and some of them only accept the SS3 encoding — sending ESC[A there types a
// literal "A" instead of moving.
//
// Only the unmodified keys move: a modified arrow (Shift-Up) stays CSI even
// under DECCKM, the way xterm itself encodes it, which is why the
// KeyShiftArrow entries of specialKeys have no counterpart here.
var applicationCursorKeys = map[gocui.Key]string{
	gocui.KeyArrowUp:    "\x1bOA",
	gocui.KeyArrowDown:  "\x1bOB",
	gocui.KeyArrowRight: "\x1bOC",
	gocui.KeyArrowLeft:  "\x1bOD",

	gocui.KeyHome: "\x1bOH",
	gocui.KeyEnd:  "\x1bOF",
}

// Normalize folds the several shapes a Ctrl-<letter> event can take into the
// single one the rest of the code expects: Key = gocui.KeyCtrl<X>, no rune, no
// control modifier.
//
// This is not defensive programming, it is required: depending on the terminal
// and on which keyboard protocol it uses, the same Ctrl-B keypress reaches us
// either as KeyCtrlB with no modifier, or as the rune 'b' with ModCtrl set —
// and in the second shape it is indistinguishable from typing a plain "b".
func Normalize(key gocui.Key, ch rune, mod gocui.Modifier) (gocui.Key, rune, gocui.Modifier) {
	if mod&modCtrl == 0 {
		return key, ch, mod
	}

	lower := unicode.ToLower(ch)
	if lower >= 'a' && lower <= 'z' {
		return gocui.KeyCtrlA + gocui.Key(lower-'a'), 0, mod &^ modCtrl
	}

	return key, ch, mod &^ modCtrl
}

// Translate encodes a gocui key event as the bytes to write to a pty. It
// returns nil when the event has no meaningful encoding (mouse events, unknown
// keys), in which case the caller should drop it.
//
// ModAlt is encoded the way terminals do it: an ESC prefix before the sequence.
//
// It encodes in normal cursor mode; a caller that knows the target
// application's DECCKM state should use TranslateWithMode instead.
func Translate(key gocui.Key, ch rune, mod gocui.Modifier) []byte {
	return TranslateWithMode(key, ch, mod, false)
}

// TranslateWithMode is Translate with the target application's cursor key mode
// taken into account: when appCursorKeys is set (the application sent
// ESC[?1h, which pkg/screen tracks as ApplicationCursorKeys), the arrows and
// Home/End are encoded as SS3 rather than CSI sequences.
func TranslateWithMode(key gocui.Key, ch rune, mod gocui.Modifier, appCursorKeys bool) []byte {
	key, ch, mod = Normalize(key, ch, mod)

	out := encode(key, ch, appCursorKeys)
	if out == nil {
		return nil
	}

	if mod&gocui.ModAlt != 0 {
		return append([]byte{esc}, out...)
	}

	return out
}

func encode(key gocui.Key, ch rune, appCursorKeys bool) []byte {
	// A printable character: gocui clears Key and reports the rune.
	if ch != 0 {
		buf := make([]byte, utf8.RuneLen(ch))
		utf8.EncodeRune(buf, ch)

		return buf
	}

	if appCursorKeys {
		if seq, ok := applicationCursorKeys[key]; ok {
			return []byte(seq)
		}
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
