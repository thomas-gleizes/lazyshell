package gui

import (
	"testing"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// newHeadlessGui builds a Gui wired to a throwaway session Manager and an
// 80x24 gocui instance that renders to an in-memory screen, so the app can be
// exercised without a real terminal or real shells (unless a test explicitly
// creates sessions on the manager).
func newHeadlessGui(t testing.TB) (*Gui, *gocui.Gui) {
	t.Helper()

	return newHeadlessGuiSized(t, 80, 24)
}

// newHeadlessGuiSized is newHeadlessGui with an explicit terminal size, for
// tests exercising layout behaviour at specific dimensions (tiny terminal,
// portrait vs. landscape).
func newHeadlessGuiSized(t testing.TB, width, height int) (*Gui, *gocui.Gui) {
	t.Helper()

	return newHeadlessGuiSizedWithConfig(t, width, height, config.Default())
}

// newHeadlessGuiSizedWithConfig is newHeadlessGuiSized with an explicit
// config, for tests exercising config-driven behaviour (keybinding remap,
// theme, panel width...).
func newHeadlessGuiSizedWithConfig(t testing.TB, width, height int, cfg config.Config) (*Gui, *gocui.Gui) {
	t.Helper()

	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode: gocui.OutputTrue,
		Headless:   true,
		Width:      width,
		Height:     height,
	})
	if err != nil {
		t.Fatalf("NewGui: %v", err)
	}
	t.Cleanup(g.Close)

	sessions := session.NewManager()
	// Shortens the SIGTERM-then-SIGKILL fallback so tests that kill a session
	// (directly or via Shutdown) do not wait on the full production timeout —
	// same reasoning as pkg/session's own tests.
	sessions.KillTimeout = 300 * time.Millisecond
	t.Cleanup(sessions.Shutdown)

	gui := New(sessions, cfg)
	gui.g = g

	return gui, g
}

func TestSetKeybindings(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.setKeybindings(g); err != nil {
		t.Fatalf("setKeybindings: %v", err)
	}

	for _, b := range gui.bindings() {
		if b.Handler == nil {
			t.Errorf("binding %v has no handler", b.Key)
		}
		if b.Description == "" {
			t.Errorf("binding %v has no description", b.Key)
		}
	}
}

func TestQuitReturnsErrQuit(t *testing.T) {
	gui := New(session.NewManager(), config.Default())

	if err := gui.quit(nil, nil); err != gocui.ErrQuit {
		t.Errorf("quit() = %v, want gocui.ErrQuit", err)
	}
}
