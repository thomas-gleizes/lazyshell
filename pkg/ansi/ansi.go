// Package ansi filters the escape sequences a pty emits down to the subset
// gocui can actually render.
//
// gocui understands SGR (colours, bold) and OSC, but not cursor positioning,
// screen erasing or DEC private modes: it prints those as literal text, so a
// modern shell prompt turns into visible garbage like "[?2004h" or "[A". Until
// a real terminal emulator lands (phase 6 of ROADMAP.md), we drop what cannot
// be rendered instead of displaying it.
//
// Dropping is lossy by design: an application redrawing in place (a prompt
// moving the cursor up, vim, htop) still produces nonsense, only readable
// nonsense. See docs/adr/0001-rendu-ansi-et-clavier.md.
package ansi

import "io"

const (
	esc = 0x1b
	bel = 0x07
)

type state int

const (
	stateText state = iota
	stateEsc
	stateCSI
	stateOSC    // ESC ] ... BEL|ST — passed through, gocui consumes them
	stateString // DCS/SOS/PM/APC — dropped whole
	stateSkip1  // charset selection and friends: skip the next byte
)

// Writer wraps a writer and strips the sequences gocui cannot render. It is
// stateful: a sequence split across two Write calls is handled correctly,
// which matters because it sits behind an io.Copy from a pty.
//
// It is not safe for concurrent use; one Writer per session, written to by
// that session's drain goroutine only.
type Writer struct {
	w io.Writer

	state state
	// seq accumulates the sequence being parsed, including the leading ESC.
	seq []byte
}

// NewWriter returns a Writer forwarding the renderable parts of p to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write always reports len(p) as written: dropped bytes are not an error, and
// reporting a short write would make io.Copy fail.
func (f *Writer) Write(p []byte) (int, error) {
	// Text runs are forwarded in one call rather than byte by byte.
	var out []byte

	for _, b := range p {
		switch f.state {
		case stateText:
			if b == esc {
				f.state = stateEsc
				f.seq = append(f.seq[:0], b)

				continue
			}

			out = append(out, b)

		case stateEsc:
			f.seq = append(f.seq, b)

			switch b {
			case '[':
				f.state = stateCSI
			case ']':
				f.state = stateOSC
			case 'P', 'X', '^', '_':
				f.state = stateString
			case '(', ')', '*', '+', '%', '#':
				f.state = stateSkip1
			default:
				// Single-byte escapes: keypad mode (ESC =, ESC >), save and
				// restore cursor (ESC 7, ESC 8), index... none renderable.
				f.state = stateText
			}

		case stateCSI:
			f.seq = append(f.seq, b)

			// A CSI sequence ends on the first byte in [0x40,0x7e].
			if b >= 0x40 && b <= 0x7e {
				if renderableCSI(f.seq) {
					out = append(out, f.seq...)
				}

				f.state = stateText
			}

		case stateOSC:
			f.seq = append(f.seq, b)

			// OSC ends on BEL or on ST (ESC \).
			if b == bel || (b == '\\' && len(f.seq) >= 2 && f.seq[len(f.seq)-2] == esc) {
				out = append(out, f.seq...)
				f.state = stateText
			}

		case stateString:
			// Dropped entirely; just look for the terminator.
			f.seq = append(f.seq, b)

			if b == bel || (b == '\\' && len(f.seq) >= 2 && f.seq[len(f.seq)-2] == esc) {
				f.state = stateText
			}

		case stateSkip1:
			f.state = stateText
		}
	}

	if len(out) > 0 {
		if _, err := f.w.Write(out); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

// renderableCSI reports whether gocui knows how to render this CSI sequence.
//
// Only two finals qualify: 'm' (SGR: colours and attributes) and 'K' (erase in
// line, which gocui turns into spaces). Private sequences (ESC[?…), which carry
// cursor visibility, bracketed paste and the alternate screen, are never
// renderable whatever their final byte.
func renderableCSI(seq []byte) bool {
	if len(seq) < 3 {
		return false
	}

	if seq[2] == '?' || seq[2] == '>' || seq[2] == '<' || seq[2] == '=' {
		return false
	}

	final := seq[len(seq)-1]

	return final == 'm' || final == 'K'
}
