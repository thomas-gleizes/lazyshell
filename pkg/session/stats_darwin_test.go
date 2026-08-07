//go:build darwin

package session

import (
	"testing"
	"time"
)

// ps(1) documents "[dd-]hh:mm:ss" but macOS actually prints "MM:SS.ss" and
// lets the minutes run past 60 — the numbers below are real observed output.
func TestParsePSTime(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  time.Duration
		valid bool
	}{
		{name: "seconds with hundredths", in: "0:00.03", want: 30 * time.Millisecond, valid: true},
		{name: "minutes", in: "7:13.09", want: 7*time.Minute + 13090*time.Millisecond, valid: true},
		{name: "minutes past an hour", in: "133:41.49", want: 133*time.Minute + 41490*time.Millisecond, valid: true},
		{name: "hours form", in: "2:03:04", want: 2*time.Hour + 3*time.Minute + 4*time.Second, valid: true},
		{name: "days form", in: "3-02:00:00", want: 74 * time.Hour, valid: true},
		{name: "not a time", in: "?", valid: false},
		{name: "too many parts", in: "1:2:3:4", valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePSTime(tc.in)
			if ok != tc.valid {
				t.Fatalf("parsePSTime(%q) ok = %v, want %v", tc.in, ok, tc.valid)
			}

			if !tc.valid {
				return
			}

			// Float seconds cannot land exactly on a nanosecond boundary.
			if diff := got - tc.want; diff > time.Millisecond || diff < -time.Millisecond {
				t.Errorf("parsePSTime(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// The linux side reports a bare comm; darwin's ps reports a full path. The two
// must agree, or the panel would show "/bin/zsh" for one and "zsh" for the
// other depending on where it runs.
func TestParsePSLineReportsABareCommand(t *testing.T) {
	got, ok := parsePSLine("36220   3264   0:00.03 /bin/zsh")
	if !ok {
		t.Fatal("parsePSLine returned not-ok on a well-formed row")
	}

	if got.PID != 36220 {
		t.Errorf("PID = %d, want 36220", got.PID)
	}

	if got.Comm != "zsh" {
		t.Errorf("Comm = %q, want %q", got.Comm, "zsh")
	}

	if got.RSSBytes != 3264*1024 {
		t.Errorf("RSSBytes = %d, want %d (ps reports kibibytes)", got.RSSBytes, 3264*1024)
	}

	// Neither is obtainable on darwin without cgo — the panel must be told
	// "unknown" rather than shown a zero.
	if got.ThreadsAvailable || got.DiskIOAvailable {
		t.Error("darwin samples must report threads and disk I/O as unavailable")
	}
}
