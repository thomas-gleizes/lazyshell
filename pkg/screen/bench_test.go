package screen

import (
	"fmt"
	"testing"
)

// fill writes lines lines of plausible, coloured shell output into s.
func fill(b *testing.B, s *Screen, lines int) {
	b.Helper()

	for i := range lines {
		if _, err := fmt.Fprintf(s, "\x1b[38;5;%dm%04d\x1b[0m  une ligne de sortie de commande\r\n", i%256, i); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

// Render is called once per session per 30 ms tick, so its cost is the
// per-frame budget of the output panel. What matters is that it depends on the
// geometry only — the 20 000 lines written here must not make it slower than
// the 24 rows on screen.
func BenchmarkRender(b *testing.B) {
	for _, geometry := range []struct {
		name       string
		cols, rows int
	}{
		{name: "80x24", cols: 80, rows: 24},
		{name: "200x50", cols: 200, rows: 50},
		{name: "300x80", cols: 300, rows: 80},
	} {
		b.Run(geometry.name, func(b *testing.B) {
			s := New(geometry.cols, geometry.rows)
			fill(b, s, 20000)

			b.ResetTimer()

			for range b.N {
				_ = s.Render()
			}
		})
	}
}

// RenderAt rebuilds a window of the scrollback cell by cell, so it is the
// expensive path — but it only runs while the user is actually scrolled back.
func BenchmarkRenderAt(b *testing.B) {
	s := New(80, 24)
	fill(b, s, 20000)

	offset := s.ScrollbackLen() / 2

	b.ResetTimer()

	for range b.N {
		_ = s.RenderAt(offset)
	}
}
