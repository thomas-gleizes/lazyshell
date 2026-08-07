package keys

import (
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/screen"
)

func TestEncodeMouseSGR(t *testing.T) {
	cases := []struct {
		name string
		ev   MouseEvent
		want string
	}{
		{
			name: "left press",
			ev:   MouseEvent{Button: MouseButtonLeft, X: 0, Y: 0, Press: true},
			want: "\x1b[<0;1;1M",
		},
		{
			// The final byte, not the button number, is what says "released" —
			// which is the whole reason SGR is worth preferring.
			name: "left release keeps its button number",
			ev:   MouseEvent{Button: MouseButtonLeft, X: 9, Y: 4},
			want: "\x1b[<0;10;5m",
		},
		{
			name: "right press",
			ev:   MouseEvent{Button: MouseButtonRight, X: 2, Y: 2, Press: true},
			want: "\x1b[<2;3;3M",
		},
		{
			name: "drag adds the motion bit",
			ev:   MouseEvent{Button: MouseButtonLeft, X: 3, Y: 1, Press: true, Motion: true},
			want: "\x1b[<32;4;2M",
		},
		{
			name: "wheel up",
			ev:   MouseEvent{Button: MouseButtonWheelUp, X: 0, Y: 0, Press: true},
			want: "\x1b[<64;1;1M",
		},
		{
			name: "wheel down",
			ev:   MouseEvent{Button: MouseButtonWheelDown, X: 0, Y: 0, Press: true},
			want: "\x1b[<65;1;1M",
		},
		{
			name: "no column limit",
			ev:   MouseEvent{Button: MouseButtonLeft, X: 400, Y: 300, Press: true},
			want: "\x1b[<0;401;301M",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(EncodeMouse(tc.ev, screen.MouseAnyEvent, true))
			if got != tc.want {
				t.Errorf("EncodeMouse() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodeMouseX10(t *testing.T) {
	cases := []struct {
		name string
		ev   MouseEvent
		want string
	}{
		{
			name: "left press, both coordinates biased by 32",
			ev:   MouseEvent{Button: MouseButtonLeft, X: 0, Y: 0, Press: true},
			want: "\x1b[M\x20\x21\x21",
		},
		{
			// The old format has no room for the button on a release: 3 just
			// means "something came up".
			name: "release loses the button number",
			ev:   MouseEvent{Button: MouseButtonRight, X: 1, Y: 1},
			want: "\x1b[M\x23\x22\x22",
		},
		{
			name: "wheel up is a press, never a release",
			ev:   MouseEvent{Button: MouseButtonWheelUp, X: 0, Y: 0, Press: true},
			want: "\x1b[M\x60\x21\x21",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(EncodeMouse(tc.ev, screen.MouseAnyEvent, false))
			if got != tc.want {
				t.Errorf("EncodeMouse() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Past column 223 a single byte cannot hold the coordinate, and there is no
// correct answer to send — so nothing is sent rather than a wrong position.
func TestEncodeMouseX10DropsPastItsCoordinateLimit(t *testing.T) {
	for _, ev := range []MouseEvent{
		{Button: MouseButtonLeft, X: 223, Y: 0, Press: true},
		{Button: MouseButtonLeft, X: 0, Y: 223, Press: true},
	} {
		if got := EncodeMouse(ev, screen.MouseAnyEvent, false); got != nil {
			t.Errorf("EncodeMouse(%+v) = %q, want nil past the 223-cell limit", ev, got)
		}
	}

	// One cell below the limit still encodes.
	ev := MouseEvent{Button: MouseButtonLeft, X: 222, Y: 222, Press: true}
	if got := EncodeMouse(ev, screen.MouseAnyEvent, false); got == nil {
		t.Error("EncodeMouse() dropped a click at the last representable cell")
	}
}

// Each mode reports strictly more than the one before it, and an event outside
// what the application asked for is not sent at all.
func TestEncodeMouseHonoursTheModeTheAppAsked(t *testing.T) {
	press := MouseEvent{Button: MouseButtonLeft, Press: true}
	release := MouseEvent{Button: MouseButtonLeft}
	drag := MouseEvent{Button: MouseButtonLeft, Press: true, Motion: true}

	cases := []struct {
		mode                             screen.MouseMode
		wantPress, wantRelease, wantDrag bool
	}{
		{screen.MouseOff, false, false, false},
		{screen.MouseX10, true, false, false},
		{screen.MouseNormal, true, true, false},
		{screen.MouseButtonEvent, true, true, true},
		{screen.MouseAnyEvent, true, true, true},
	}

	for _, tc := range cases {
		if got := EncodeMouse(press, tc.mode, true) != nil; got != tc.wantPress {
			t.Errorf("mode %d: press encoded = %v, want %v", tc.mode, got, tc.wantPress)
		}

		if got := EncodeMouse(release, tc.mode, true) != nil; got != tc.wantRelease {
			t.Errorf("mode %d: release encoded = %v, want %v", tc.mode, got, tc.wantRelease)
		}

		if got := EncodeMouse(drag, tc.mode, true) != nil; got != tc.wantDrag {
			t.Errorf("mode %d: drag encoded = %v, want %v", tc.mode, got, tc.wantDrag)
		}
	}
}
