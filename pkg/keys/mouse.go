package keys

// The mouse tracking mode is pkg/screen's, imported rather than mirrored here:
// it is emulator state, the emulator is what decides its meaning, and a second
// copy of the enum is exactly the kind of thing that drifts. The dependency is
// acyclic — pkg/screen knows nothing of pkg/keys.
import (
	"fmt"

	"github.com/thomas-gleizes/lazyshell/pkg/screen"
)

// MouseButton is which button an event is about, in the numbering the wire
// format itself uses — so the encoders below stay a formatting exercise rather
// than a translation one.
type MouseButton int

// The buttons lazyshell can report. Wheel notches are buttons too in this
// protocol, from bit 6 upwards.
const (
	MouseButtonLeft      MouseButton = 0
	MouseButtonMiddle    MouseButton = 1
	MouseButtonRight     MouseButton = 2
	MouseButtonWheelUp   MouseButton = 64
	MouseButtonWheelDown MouseButton = 65
)

// MouseEvent is one event to hand to the program running in a session.
// Coordinates are zero-based cells within the session's screen — EncodeMouse
// converts to the one-based numbering the wire format uses.
type MouseEvent struct {
	Button MouseButton
	X, Y   int
	// Press is false for a button being released. Ignored for wheel notches,
	// which have no release.
	Press bool
	// Motion marks the pointer having moved with the button held down.
	Motion bool
}

// motionBit is added to the button number to mark a drag, per the protocol.
const motionBit = 32

// EncodeMouse renders an event in the form the application asked for, or nil
// when this event is not one it asked to hear about.
//
// Two encodings exist and the application picks. SGR (DECSET 1006) is the one
// everything modern sets, and the only one with no coordinate limit. The
// historical X10 form packs each coordinate into a single byte biased by 32,
// which caps it at column 223 — past that there is nothing correct to send, so
// nothing is sent rather than a wrong position.
func EncodeMouse(ev MouseEvent, mode screen.MouseMode, sgr bool) []byte {
	if !wants(ev, mode) {
		return nil
	}

	button := int(ev.Button)
	if ev.Motion {
		button += motionBit
	}

	// Coordinates are one-based on the wire.
	x, y := ev.X+1, ev.Y+1
	if x < 1 || y < 1 {
		return nil
	}

	if sgr {
		final := 'M'
		if !ev.Press && !isWheel(ev.Button) {
			final = 'm'
		}

		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x, y, final))
	}

	return encodeX10(ev, button, x, y)
}

// encodeX10 is the pre-SGR form: ESC [ M, then three bytes holding the button
// and the two coordinates, each biased by 32.
//
// A release carries no button number at all here — 3 means "some button came
// up", which is why forwarding needs SGR to say *which*. And since a coordinate
// has one byte, anything past column or row 223 is unrepresentable.
func encodeX10(ev MouseEvent, button, x, y int) []byte {
	const (
		bias    = 32
		maxCell = 255 - bias // 223
	)

	if x > maxCell || y > maxCell {
		return nil
	}

	if !ev.Press && !isWheel(ev.Button) {
		button = 3
	}

	return []byte{esc, '[', 'M', byte(button + bias), byte(x + bias), byte(y + bias)}
}

// wants reports whether the mode the application armed covers this event. The
// modes are cumulative in what they report: presses, then releases, then drags.
func wants(ev MouseEvent, mode screen.MouseMode) bool {
	switch mode {
	case screen.MouseOff:
		return false
	case screen.MouseX10:
		// Presses only — no releases, no motion. The wheel has no release, so
		// a notch is a press and goes through.
		return ev.Press && !ev.Motion
	case screen.MouseNormal:
		return !ev.Motion
	case screen.MouseButtonEvent, screen.MouseAnyEvent:
		return true
	default:
		return false
	}
}

// isWheel reports whether a button is a wheel notch, which is reported as a
// press and never as a release.
func isWheel(button MouseButton) bool {
	return button == MouseButtonWheelUp || button == MouseButtonWheelDown
}
