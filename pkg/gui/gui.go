// Package gui owns everything terminal-facing: the gocui bootstrap, the
// layout, the keybindings and the rendering loop.
package gui

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
	"github.com/thomas-gleizes/lazyshell/pkg/tasks"
)

// reRenderInterval is the rate at which the sessions panel is refreshed
// (session statuses change asynchronously in the background) and at which a
// selected session's output is re-rendered. Same value as lazydocker.
const reRenderInterval = 30 * time.Millisecond

// statusHint is shown in the status bar as long as there is no error to
// report and the output panel is not in pass-through mode.
const statusHint = " n: nouvelle session   x/d: tuer   j/k: naviguer   Tab: changer de focus   ?: aide   q: quitter "

// Gui holds the gocui instance and the state of the interface.
type Gui struct {
	g *gocui.Gui

	sessions    *session.Manager
	outputTasks *tasks.Manager
	focus       *focusManager

	// theme holds every color the UI draws chrome with, resolved from
	// pkg/config's Theme at construction time.
	theme Theme

	// prefixKey is the pass-through escape prefix, see prefixFrom.
	prefixKey gocui.Key
	// configuredShell is pkg/config's Shell, used by newSession when
	// non-empty instead of falling back to $SHELL/bash.
	configuredShell string
	// sessionsPanelWidth is pkg/config's SessionsPanelWidth: the sessions
	// panel's width in landscape mode (columns), height in portrait mode
	// (rows). See pkg/gui/layout.go's rootBox.
	sessionsPanelWidth int
	// keymap is pkg/config's Keybindings: action id -> gocui.Parse key spec,
	// consulted by resolveBinding for every Binding with a non-empty Action.
	keymap map[string]string
	// helpReturnView is the view that was current when showHelp opened the
	// help popup, so closeHelp can restore focus to it rather than always
	// landing back on the sessions panel.
	helpReturnView string
	// promptReturnView is helpReturnView's equivalent for showPrompt's popup.
	promptReturnView string
	// promptOnSubmit is the callback showPrompt is currently waiting on,
	// captured at open time and consumed (then cleared) by submitPrompt.
	promptOnSubmit func(string) error
	// prefixPending and passThroughActive are only ever touched from
	// editOutput, called synchronously from gocui's own event dispatch —
	// always the same goroutine, no mutex needed.
	prefixPending     bool
	passThroughActive bool

	// mu guards selectedIndex and scrollOffset: both are written from
	// gocui's main goroutine (keybinding handlers, the output Editor) but
	// also read from background goroutines — goEvery's ticker for
	// selectedIndex, nothing currently for scrollOffset but it is captured
	// by output.go at task-start time from the same call sites, so the same
	// guard is reused for both rather than reasoning about two policies.
	mu sync.Mutex
	// selectedIndex is the currently highlighted line in the sessions panel.
	selectedIndex int
	// scrollOffset is how many lines the output panel is scrolled back from
	// the live bottom; 0 means "live". Reset to 0 whenever the selection
	// changes or pass-through is (re-)armed.
	scrollOffset int

	// sessionCounter feeds the default name of sessions created with "n".
	sessionCounter int
	// lastError, if non-empty, is shown in the status bar instead of the
	// keybinding hint until the next successful action.
	lastError string

	// PauseBackgroundThreads stops the periodic tasks started by goEvery, for
	// when the terminal is handed over to another process.
	PauseBackgroundThreads bool
}

// New allocates the Gui around an already-running session Manager and a
// loaded configuration. It does not touch the terminal: that only happens in
// Run.
func New(sessions *session.Manager, cfg config.Config) *Gui {
	return &Gui{
		sessions:           sessions,
		outputTasks:        tasks.NewManager(),
		focus:              newFocusManager(),
		theme:              newTheme(cfg.Theme),
		prefixKey:          prefixFrom(cfg.PrefixKey),
		configuredShell:    cfg.Shell,
		sessionsPanelWidth: cfg.SessionsPanelWidth,
		keymap:             cfg.Keybindings,
	}
}

// getSelectedIndex/setSelectedIndex, getScrollOffset/setScrollOffset are the
// only safe way to touch these two fields from a goroutine other than
// gocui's own — see the mu field comment.
func (gui *Gui) getSelectedIndex() int {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	return gui.selectedIndex
}

func (gui *Gui) setSelectedIndex(i int) {
	gui.mu.Lock()
	gui.selectedIndex = i
	gui.mu.Unlock()
}

func (gui *Gui) getScrollOffset() int {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	return gui.scrollOffset
}

func (gui *Gui) setScrollOffset(offset int) {
	gui.mu.Lock()
	gui.scrollOffset = offset
	gui.mu.Unlock()
}

// Run initialises gocui and blocks in the main loop until the user quits. The
// terminal is always restored before returning, including on panic.
func (gui *Gui) Run() (err error) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.OutputNormal,
		SupportOverlaps: false,
	})
	if err != nil {
		return fmt.Errorf("failed to initialise the terminal: %w", err)
	}

	// Deferred calls run last-in-first-out: g.Close() restores the terminal
	// first, then this handler turns a panic into a plain error the caller can
	// print on a sane terminal.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n\n%s", r, debug.Stack())
		}
	}()
	defer g.Close()

	gui.g = g
	g.Cursor = false
	g.Mouse = false

	// Highlight draws the current view's frame in SelFrameColor — the only
	// code needed for an "active panel" border, gocui does the rest on every
	// SetCurrentView. FrameColor is every other (inactive) view's frame.
	g.Highlight = true
	g.SelFrameColor = gui.theme.ActiveBorderColor
	g.FrameColor = gui.theme.InactiveBorderColor

	// SetManager purges existing keybindings, so it must run before
	// setKeybindings. The second manager (focus) touches no view; it only
	// detects focus changes gocui itself has no event for.
	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)

	if err := gui.setKeybindings(g); err != nil {
		return err
	}

	gui.goEvery(reRenderInterval, gui.renderSessionsPanel)

	if err := g.MainLoop(); err != nil && !goerrors.Is(err, gocui.ErrQuit) {
		return err
	}

	gui.outputTasks.Stop()

	return nil
}

// renderStatus writes the status bar's content: the last error takes
// priority, then the pass-through indicator, then the keybinding hint.
// Without a clear indicator the user cannot tell whether q quits the app or
// goes to the shell. Safe to call directly (no g.Update) whenever the caller
// is already running on gocui's main goroutine — a keybinding handler, the
// output Editor, or initView during layout.
func (gui *Gui) renderStatus(view *gocui.View) {
	view.Clear()

	text := statusHint

	switch {
	case gui.lastError != "":
		text = " " + gui.lastError + " "
	case gui.passThroughActive:
		text = fmt.Sprintf(" -- INSERT --  (%s pour sortir) ", prefixName(gui.prefixKey))
	}

	fmt.Fprint(view, text)
}

// goEvery runs fn once, then on every tick of interval until the process
// exits. Ported from lazydocker's helper of the same name.
func (gui *Gui) goEvery(interval time.Duration, fn func() error) {
	_ = fn()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if !gui.PauseBackgroundThreads {
				_ = fn()
			}
		}
	}()
}
