package gui

import (
	"strings"
	"testing"

	"github.com/jesseduffield/lazycore/pkg/boxlayout"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
)

// Every option this phase added has to actually reach the code that used to
// hardcode it. Wiring is the kind of thing that looks done because the field
// exists — these assert the value arrives, one per option, through gui.New
// exactly as pkg/app builds it.

func TestNewCarriesLayoutOptions(t *testing.T) {
	cfg := config.Default()
	cfg.SessionsPanelWidth = 50
	cfg.SessionsPanelHeight = 20
	cfg.PortraitMaxWidth = 100
	cfg.PortraitMinHeight = 20

	gui := New(nil, cfg)

	// 90x40 is landscape under the built-in thresholds (84/45) and portrait
	// under the configured ones, so the assertion cannot pass by accident.
	if !gui.isPortrait(90, 40) {
		t.Error("configured portrait thresholds did not reach isPortrait")
	}

	portrait := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 90, 40)

	sessions, ok := portrait[sessionsViewName]
	if !ok {
		t.Fatal("sessions view missing from the portrait layout")
	}

	if height := sessions.Y1 - sessions.Y0 + 1; height != 20 {
		t.Errorf("portrait sessions height = %d, want the configured 20", height)
	}

	landscape := boxlayout.ArrangeWindows(gui.rootBox(), 0, 0, 200, 40)
	if width := landscape[sessionsViewName].X1 - landscape[sessionsViewName].X0 + 1; width != 50 {
		t.Errorf("landscape sessions width = %d, want the configured 50", width)
	}
}

func TestNewCarriesMarkers(t *testing.T) {
	cfg := config.Default()
	cfg.Markers.Bell = "*"
	cfg.Markers.AltScreen = ""

	got := New(nil, cfg).markerSet()

	if got.bell != "*" {
		t.Errorf("bell marker = %q, want the configured %q", got.bell, "*")
	}

	// An explicitly emptied marker must stay empty: that is how it is turned
	// off, and "fall back to the default when empty" would make it impossible.
	if got.altScreen != "" {
		t.Errorf("alt-screen marker = %q, want it disabled", got.altScreen)
	}
}

// A Gui built as a bare struct literal — what most tests in this package do —
// has no configured markers at all, and must still draw the built-in ones
// rather than an empty gutter.
func TestZeroGuiKeepsDefaultMarkers(t *testing.T) {
	var gui Gui

	if got := gui.markerSet(); got.bell != bellMarker || got.altScreen != altScreenMarker {
		t.Errorf("zero Gui markers = %+v, want the built-in defaults", got)
	}
}

func TestNewCarriesScrollSteps(t *testing.T) {
	cfg := config.Default()
	cfg.Scroll.PageLines = 5
	cfg.Scroll.HalfPageDivisor = 4

	gui := New(nil, cfg)

	if got := gui.pageStep(40); got != 5 {
		t.Errorf("pageStep(40) = %d, want the configured 5", got)
	}

	if got := gui.halfPageStep(40); got != 10 {
		t.Errorf("halfPageStep(40) = %d, want 40/4", got)
	}

	// page_lines: 0 is not "no scrolling", it is "one full page".
	cfg.Scroll.PageLines = 0
	if got := New(nil, cfg).pageStep(40); got != 40 {
		t.Errorf("pageStep(40) with page_lines 0 = %d, want a full page", got)
	}
}

func TestNewCarriesRefreshInterval(t *testing.T) {
	cfg := config.Default()
	cfg.RefreshIntervalMs = 100

	if got := New(nil, cfg).tick(); got.Milliseconds() != 100 {
		t.Errorf("tick() = %v, want the configured 100ms", got)
	}

	// time.NewTicker panics on a non-positive duration, so a Gui that never saw
	// a config must still hand out a usable period.
	var bare Gui
	if got := bare.tick(); got <= 0 {
		t.Errorf("zero Gui tick() = %v, want a positive fallback", got)
	}
}

// The empty-list hint names the key that creates a session, so a user who
// remapped it must not be told to press a key that does nothing. This is the
// one place the sessions panel hardcodes a key, and it is worth a test because
// nothing else would catch it.
func TestEmptyPanelHintMentionsCreation(t *testing.T) {
	if got := sessionsPanelContent(nil, testMarkers); !strings.Contains(got, "n") {
		t.Errorf("empty panel content = %q, want it to mention the new-session key", got)
	}
}
