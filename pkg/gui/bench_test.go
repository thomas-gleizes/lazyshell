package gui

import (
	"fmt"
	"testing"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// benchSessions creates n sessions and fills each one's emulator with
// plausible coloured output, without going through a real shell.
func benchSessions(b *testing.B, gui *Gui, n int) []*session.Session {
	b.Helper()

	sessions := make([]*session.Session, 0, n)

	for i := range n {
		sess, err := gui.sessions.New(fmt.Sprintf("s%d", i), "/bin/sh")
		if err != nil {
			b.Fatalf("New: %v", err)
		}

		for j := range 2000 {
			if _, err := fmt.Fprintf(sess.Screen(), "\x1b[38;5;%dm%04d\x1b[0m  sortie\r\n", j%256, j); err != nil {
				b.Fatalf("Screen().Write: %v", err)
			}
		}

		sessions = append(sessions, sess)
	}

	return sessions
}

// buildOutputFrame is what the render task does on every 30 ms tick. Only the
// selected session is rendered, whatever the number of sessions running, which
// is the property this benchmark exists to keep honest.
func BenchmarkBuildOutputFrame(b *testing.B) {
	gui, _ := newHeadlessGui(b)

	sessions := benchSessions(b, gui, 1)

	b.ResetTimer()

	for range b.N {
		_ = buildOutputFrame(sessions[0], 0, true, "", -1, -1)
	}
}

// sessionsPanelContent runs on the same ticker and does scale with the number
// of sessions, since it reads every one's emulator state.
func BenchmarkSessionsPanelContent(b *testing.B) {
	for _, n := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("%d-sessions", n), func(b *testing.B) {
			gui, _ := newHeadlessGui(b)

			sessions := benchSessions(b, gui, n)

			b.ResetTimer()

			for range b.N {
				_ = sessionsPanelContent(sessions, testMarkers, "", nil)
			}
		})
	}
}
