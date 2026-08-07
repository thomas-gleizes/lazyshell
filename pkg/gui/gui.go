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

	"github.com/thomas-gleizes/lazyshell/pkg/agent"
	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/i18n"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
	"github.com/thomas-gleizes/lazyshell/pkg/tasks"
)

// reRenderInterval is the default rate at which the sessions panel is refreshed
// (session statuses change asynchronously in the background) and at which a
// selected session's output is re-rendered. Same value as lazydocker; overridden
// by pkg/config's RefreshIntervalMs, which is what Gui.refreshInterval carries.
const reRenderInterval = 30 * time.Millisecond

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

	// tr resolves every user-facing string against pkg/config's Language. Nil
	// on a bare Gui{} literal (most tests): Catalog.T is nil-safe and falls
	// back to French, matching what those tests already assert on.
	tr *i18n.Catalog

	// theme holds every color the UI draws chrome with, resolved from
	// pkg/config's Theme at construction time.
	theme Theme

	// prefixKey is the pass-through escape prefix, see prefixFrom.
	prefixKey gocui.Key
	// configuredShell is pkg/config's Shell, used by newSession when
	// non-empty instead of falling back to $SHELL/bash.
	configuredShell string
	// sessionsPanelWidth/sessionsPanelHeight are the sessions panel's size:
	// a width in landscape mode (columns), a height in portrait mode (rows).
	// See pkg/gui/layout.go's rootBox.
	sessionsPanelWidth  int
	sessionsPanelHeight int
	// portraitMaxWidth/portraitMinHeight are the geometry at which the layout
	// stacks the panels instead of splitting them — see isPortrait.
	portraitMaxWidth  int
	portraitMinHeight int
	// refreshInterval is pkg/config's RefreshIntervalMs: the redraw tick used
	// by goEvery for the sessions panel and by showOutput for the output one.
	refreshInterval time.Duration
	// markers are the sessions list's gutter characters, and scroll the output
	// panel's scrolling steps — both straight from pkg/config.
	markers config.Markers
	scroll  config.Scroll
	// clipboardFallback is pkg/config's Clipboard.FallbackCommand — see
	// pkg/gui/clipboard.go's copyToClipboard.
	clipboardFallback string
	// notifyFallback is pkg/config's Notify.FallbackCommand — see
	// pkg/gui/notify.go's notifyAgentState.
	notifyFallback string
	// agentStatsCommand is pkg/config's AgentStatsCommand — see
	// pkg/gui/stats.go's refreshAgentStats.
	agentStatsCommand string
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
	// zoomed hides the sessions panel and gives the output panel the whole
	// screen. Only ever touched from gocui's own goroutine (toggleZoom, a
	// keybinding handler), same reasoning as passThroughActive above.
	zoomed bool

	// searchPattern, searchMatches and searchIndex are the scrollback-search
	// state (pkg/gui/search.go): "" means no search is active. Only ever
	// touched from gocui's own goroutine (editDuringScroll, the search
	// handlers), same reasoning as passThroughActive above. searchMatches
	// holds absolute line indices from Screen.Find; searchIndex is the
	// position within it that n/N last jumped to.
	searchPattern string
	searchMatches []int
	searchIndex   int

	// filterPattern is the sessions-list filter (pkg/gui/filter.go): "" means
	// no filter, every session is shown. Same concurrency rule as
	// searchPattern — only ever touched from gocui's own goroutine (the "/"
	// binding on sessionsViewName and the prompt it opens).
	filterPattern string

	// copyModeActive, copyAnchorLine and copyCursorLine are copy-mode's state
	// (pkg/gui/copymode.go): a whole-line selection in the output panel,
	// addressed in the same absolute-line coordinates as searchMatches
	// (Screen.Find's contract). Same concurrency rule as passThroughActive —
	// only ever touched from gocui's own goroutine (editDuringScroll's 'v'/
	// 'y'/Esc/movement handlers).
	copyModeActive bool
	copyAnchorLine int
	copyCursorLine int

	// broadcastMarks is the set of session IDs currently marked to receive
	// broadcast keystrokes (pkg/gui/broadcast.go) — nil/empty means no marks
	// at all, the common case. Marking is armed (a keystroke actually gets
	// duplicated) only once 2 or more sessions are marked; a single mark is
	// just a "waiting to pick a second one" state, not yet broadcasting.
	// Same concurrency rule as passThroughActive — only ever touched from
	// gocui's own goroutine (the "b" binding on sessionsViewName).
	broadcastMarks map[string]bool

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

	// notifiedState is the last agent.State a notification was fired for,
	// per session id (pkg/gui/notify.go) — written and read only from
	// checkAgentNotifications' own goEvery tick, but guarded by mu anyway
	// since renderSessionsPanel's goroutine reads sessions concurrently and
	// a future caller should not have to rediscover this rule.
	notifiedState map[string]agent.State

	// statsSessionID/statsLine/statsCheckedAt cache AgentStatsCommand's last
	// output (pkg/gui/stats.go): which session it was computed for, the
	// trimmed first line of its stdout, and when — refreshAgentStats' own
	// throttle. Read by sessionsPanelContent's caller to build its
	// statsLines argument.
	statsSessionID string
	statsLine      string
	statsCheckedAt time.Time
	statsPending   bool

	// sessionCounter feeds the default name of sessions created with "n".
	sessionCounter int
	// lastError, if non-empty, is shown in the status bar instead of the
	// keybinding hint until the next successful action.
	lastError string
	// lastInfo is lastError's positive counterpart — a transient success
	// message (currently just the export path) shown in its place. The two
	// are kept as separate fields rather than one "last message" so a
	// reader is never left guessing which kind is currently set; each
	// setter (reportSessionError/reportSessionInfo, sessions_panel.go)
	// clears the other, so an old message of one kind never lingers behind
	// a new one of the other kind.
	lastInfo string

	// PauseBackgroundThreads stops the periodic tasks started by goEvery, for
	// when the terminal is handed over to another process.
	PauseBackgroundThreads bool
}

// New allocates the Gui around an already-running session Manager and a
// loaded configuration. It does not touch the terminal: that only happens in
// Run.
func New(sessions *session.Manager, cfg config.Config) *Gui {
	return &Gui{
		sessions:            sessions,
		outputTasks:         tasks.NewManager(),
		focus:               newFocusManager(),
		tr:                  i18n.New(cfg.Language),
		theme:               newTheme(cfg.Theme),
		prefixKey:           prefixFrom(cfg.PrefixKey),
		configuredShell:     cfg.Shell,
		sessionsPanelWidth:  cfg.SessionsPanelWidth,
		sessionsPanelHeight: cfg.SessionsPanelHeight,
		portraitMaxWidth:    cfg.PortraitMaxWidth,
		portraitMinHeight:   cfg.PortraitMinHeight,
		refreshInterval:     refreshIntervalFrom(cfg.RefreshIntervalMs),
		markers:             cfg.Markers,
		scroll:              cfg.Scroll,
		clipboardFallback:   cfg.Clipboard.FallbackCommand,
		notifyFallback:      cfg.Notify.FallbackCommand,
		agentStatsCommand:   cfg.AgentStatsCommand,
		keymap:              cfg.Keybindings,
	}
}

// refreshIntervalFrom turns pkg/config's milliseconds into a duration, falling
// back to reRenderInterval for a zero value. Zero is not a user choice here —
// Config.Validate rejects it — but a Gui built from a config.Config{} literal
// (several tests in this package do exactly that) must still tick.
func refreshIntervalFrom(ms int) time.Duration {
	if ms <= 0 {
		return reRenderInterval
	}

	return time.Duration(ms) * time.Millisecond
}

// tick is the redraw period to use, guarding the one thing that would turn a
// zero value into a crash rather than a default: time.NewTicker panics on a
// non-positive duration, and tests build a Gui as a bare struct literal.
func (gui *Gui) tick() time.Duration {
	if gui.refreshInterval <= 0 {
		return reRenderInterval
	}

	return gui.refreshInterval
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

	// Required for View.Footer to be drawn at all: gocui gates the bottom-frame
	// text on this flag (its own use for it is a "1 of 20" list counter, but the
	// mechanism is just "draw this string on the frame's bottom line").
	g.ShowListFooter = true

	// SetManager purges existing keybindings, so it must run before
	// setKeybindings. The second manager (focus) touches no view; it only
	// detects focus changes gocui itself has no event for.
	g.SetManager(gocui.ManagerFunc(gui.layout), gui.focus)

	if err := gui.setKeybindings(g); err != nil {
		return err
	}

	gui.goEvery(gui.tick(), gui.renderSessionsPanel)

	// A separate tick from renderSessionsPanel's, on purpose: a transition
	// into blocked/done must be caught even on a render that gets skipped by
	// its own content-diffing, and this must never touch that function's
	// perf-tested diff logic. checkAgentNotifications and refreshAgentStats
	// share this one tick (rather than each getting their own) since neither
	// does anything expensive itself — refreshAgentStats' own throttle is
	// what actually bounds the cost of the command it may spawn.
	gui.goEvery(gui.tick(), func() error {
		_ = gui.checkAgentNotifications()

		return gui.refreshAgentStats()
	})

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
// priority, then a transient success message (lastInfo), then pass-through,
// then copy-mode, then search, then the sessions-list filter, then the
// keybinding hint. The alt-screen marker is appended to whichever of those
// is shown, and the broadcast warning — if armed — is prepended in front of
// all of it: broadcast is the one state dangerous enough that it must stay
// visible no matter what else the status bar is currently saying, pass-
// through included, since pass-through is exactly when it does something.
// Without a clear indicator the user cannot tell whether q quits the app or
// goes to the shell. Safe to call directly (no g.Update) whenever the
// caller is already running on gocui's main goroutine — a keybinding
// handler, the output Editor, or initView during layout.
func (gui *Gui) renderStatus(view *gocui.View) {
	view.Clear()

	text := gui.tr.T("status.hint")

	switch {
	case gui.lastError != "":
		text = " " + gui.lastError + " "
	case gui.lastInfo != "":
		text = " " + gui.lastInfo + " "
	case gui.passThroughActive:
		text = gui.tr.T("status.passthrough", prefixName(gui.prefixKey))
	case gui.copyModeActive:
		from, to := gui.copySelectionRange()
		text = gui.tr.T("status.copymode", to-from+1)
	case gui.searchActive():
		text = gui.tr.T("status.search", gui.searchPattern, gui.searchIndex+1, len(gui.searchMatches))
	case gui.filterActive():
		text = gui.tr.T("status.filter", gui.filterPattern, len(gui.filteredSessions()))
	}

	if gui.selectedIsAltScreen() {
		text += altScreenIndicator + " "
	}

	if sess := gui.selectedSession(); sess != nil && gui.broadcastArmed(sess.ID) {
		text = gui.tr.T("status.broadcast", len(gui.broadcastMarks)) + text
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
