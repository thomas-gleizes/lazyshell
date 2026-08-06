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

// altScreenIndicator marks a session whose alternate screen is active, i.e. a
// full-screen application (vim, htop, less) is in control. Shown in the status
// bar for the selected session, and in the sessions list gutter for every one.
const altScreenIndicator = "[ALT]"

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
	// lastSessionsContent/lastSessionsSelected/sessionsDrawn memoise what the
	// sessions panel last pushed, so an unchanged panel does not trigger a
	// full-screen redraw on every tick. Under the same mu because
	// renderSessionsPanel runs from both goEvery's goroutine and gocui's.
	lastSessionsContent  string
	lastSessionsSelected int
	sessionsDrawn        bool

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

// SetStartupError records a problem that happened during bootstrap — a project
// file that could not be read, a session that failed to start — so it is shown
// in the status bar as soon as the interface comes up. Must be called before
// Run: renderStatus already gives lastError priority over everything else.
func (gui *Gui) SetStartupError(msg string) {
	gui.lastError = msg
}

// StartupError reports what SetStartupError recorded, so pkg/app's bootstrap
// tests can assert on what the user will be told without standing up a
// terminal.
func (gui *Gui) StartupError() string {
	return gui.lastError
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
	// OutputTrue is required, not cosmetic, and this is the whole reason
	// full-screen applications did not work before phase 6: below it, gocui's
	// escape interpreter rejects the 256-colour and truecolour SGR forms
	// (escape.go's csiColor) and prints their body as literal text — "[38;5;2m"
	// in the middle of the screen. pkg/screen emits exactly those forms for any
	// themed prompt, vim colorscheme or htop bar. Measured in ADR 0001,
	// validated in cmd/spike-pty, and only now carried over to the real app.
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.OutputTrue,
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
	// The cursor is off by default and turned on by the output render task
	// only while a session is actually being typed into — see showOutput.
	g.Cursor = false
	g.Mouse = false
	// Without InputEsc, a lone Esc is held back while the input parser waits to
	// see whether an escape sequence follows. Esc is vim's central key, so it
	// has to be delivered as itself; same setting as cmd/spike-pty.
	g.InputEsc = true

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

	// Sessions can already exist before the first keypress — pkg/app starts the
	// ones a project file declares. Nothing else calls onSelectionChanged until
	// the user moves the selection, so without this the output panel would stay
	// blank while three sessions are demonstrably running.
	if len(gui.sessions.List()) > 0 {
		gui.onSelectionChanged()
	}

	if err := g.MainLoop(); err != nil && !goerrors.Is(err, gocui.ErrQuit) {
		return err
	}

	gui.outputTasks.Stop()

	return nil
}

// renderStatus writes the status bar's content: the last error takes
// priority, then the pass-through indicator, then the keybinding hint. The
// alt-screen marker is appended to whichever of those is shown. Without a
// clear indicator the user cannot tell whether q quits the app or goes to the
// shell. Safe to call directly (no g.Update) whenever the caller is already
// running on gocui's main goroutine — a keybinding handler, the output
// Editor, or initView during layout.
func (gui *Gui) renderStatus(view *gocui.View) {
	view.Clear()

	text := statusHint

	switch {
	case gui.lastError != "":
		text = " " + gui.lastError + " "
	case gui.passThroughActive:
		text = fmt.Sprintf(" -- INSERT --  (%s pour sortir) ", prefixName(gui.prefixKey))
	}

	if gui.selectedIsAltScreen() {
		text += altScreenIndicator + " "
	}

	fmt.Fprint(view, text)
}

// selectedIsAltScreen reports whether the selected session has a full-screen
// application in control (vim, htop, less). lazyshell deliberately does not
// change mode on its own when this flips — it only says so, in the status bar
// and in the sessions list, and stops offering a scrollback that the alternate
// screen does not feed.
func (gui *Gui) selectedIsAltScreen() bool {
	sess := gui.selectedSession()

	return sess != nil && sess.Screen().IsAltScreen()
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
