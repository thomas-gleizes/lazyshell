package gui

import (
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/keys"
	"github.com/thomas-gleizes/lazyshell/pkg/screen"
)

// Mouse support. Unlike keyboard bindings, these go through
// g.SetViewClickBinding rather than g.SetKeybinding: it is the only API that
// hands the handler the coordinates of the click (ViewMouseBindingOpts.X/Y,
// already converted to content coordinates) and whether it was a double one.
// SetKeybinding would only say "a button was pressed somewhere".
//
// Three rules shape everything below.
//
//  1. A mouse event must never reach a session as a keystroke. gocui gives
//     MouseLeft and KeyShiftArrowDown the same value, so an event that falls
//     through to the output view's Editor is typed into the shell as
//     "\x1b[1;2B". editOutput drops mouse keys for that reason, and
//     shouldHandleMouseEvent below stops them one step earlier still.
//
//  2. The wheel scrolls the *content*, never the command history. This is the
//     behaviour the feature was asked for: on a shell or an AI agent CLI, a
//     wheel notch must move lazyshell's scrollback, and must never be encoded
//     as an Up/Down arrow — an arrow at a prompt recalls the previous command
//     instead of scrolling, which is precisely the annoyance being fixed. The
//     wheel only leaves lazyshell when the program inside the session has
//     explicitly asked for mouse reporting (forwardMouseToApp), which a shell
//     and an agent CLI never do. When the alternate screen is up and no such
//     request was made, the wheel does nothing: there is no scrollback behind a
//     full-screen application, so doing nothing is the correct answer, not an
//     omission.
//
//  3. A single click is navigation, never engagement. Clicking a session
//     selects it without arming pass-through, mirroring the rule j/k already
//     follow (see sessions_panel.go's focusSelectedShell). Double-click is the
//     deliberate gesture, the mouse's equivalent of Enter.

// mouseBindings returns the click/wheel bindings, the mouse counterpart of
// bindings(). Kept separate because gocui keeps two distinct registries and
// SetViewClickBinding takes a different handler shape.
//
// They are not remappable through config.Keybindings: that map addresses
// actions by key spec, and a click carries coordinates a key spec cannot
// express. The mouse's gestures are fixed; the keyboard's are not.
func (gui *Gui) mouseBindings() []*gocui.ViewMouseBinding {
	return []*gocui.ViewMouseBinding{
		{
			ViewName: sessionsViewName,
			Key:      gocui.MouseLeft,
			Handler:  gui.clickSession,
		},
		{
			ViewName: sessionsViewName,
			Key:      gocui.MouseWheelUp,
			Handler:  gui.wheelSessions(-1),
		},
		{
			ViewName: sessionsViewName,
			Key:      gocui.MouseWheelDown,
			Handler:  gui.wheelSessions(1),
		},
		{
			ViewName: outputViewName,
			Key:      gocui.MouseLeft,
			Handler:  gui.clickOutput,
		},
		{
			ViewName: helpViewName,
			Key:      gocui.MouseLeft,
			Handler:  gui.clickHelp,
		},
		{
			ViewName: groupPickerViewName,
			Key:      gocui.MouseLeft,
			Handler:  gui.clickGroupPicker,
		},
		{
			// The drag: gocui reports a moved-with-the-button-down event as
			// MouseLeft carrying ModMotion, so it needs its own entry — a
			// binding with Modifier ModNone does not match it.
			ViewName: outputViewName,
			Key:      gocui.MouseLeft,
			Modifier: gocui.ModMotion,
			Handler:  gui.dragOutput,
		},
		{
			ViewName: outputViewName,
			Key:      gocui.MouseWheelUp,
			Handler:  gui.wheelOutput(1),
		},
		{
			ViewName: outputViewName,
			Key:      gocui.MouseWheelDown,
			Handler:  gui.wheelOutput(-1),
		},
	}
}

// setMouseBindings registers them. Called from setKeybindings, so it inherits
// the same ordering constraint: SetManager purges the mouse registry too.
func (gui *Gui) setMouseBindings(g *gocui.Gui) error {
	if !gui.mouse.Enabled {
		return nil
	}

	for _, binding := range gui.mouseBindings() {
		// Same instrumentation point as setKeybindings' loop, for the other
		// registry — loggedMouse hands the binding straight back when --debug
		// is off.
		if err := g.SetViewClickBinding(gui.loggedMouse(binding)); err != nil {
			return err
		}
	}

	return nil
}

// shouldHandleMouseEvent is gocui's veto hook, consulted before it does
// anything at all with an event — including moving the clicked view's cursor,
// which on the output view would fight the emulator for control of it.
//
// Everything on the output view is refused while pass-through is armed and the
// session's program has not asked for the mouse: in that state a click has no
// meaning lazyshell could act on, and letting gocui proceed would move a cursor
// the emulator owns. When the program *has* asked, the event is accepted so
// forwardMouseToApp can encode it.
func (gui *Gui) shouldHandleMouseEvent(view *gocui.View, _ gocui.Key) bool {
	if view == nil {
		return true
	}

	if view.Name() == outputViewName && gui.passThroughActive {
		return gui.appWantsMouse()
	}

	return true
}

// clickSession selects the session on the clicked line, and focuses its shell
// on a double click.
//
// opts.Y is a line index within the view's content, and the mouse is the only
// input that arrives that way — so this is one of the two places (the other is
// renderSessionsPanel) that converts between line numbers and session indexes.
// sessionIndexForRowLine answers -1 for a line that is not a session's: a
// group header, the "no sessions" placeholder, or past the end of the list.
// Clicking a header is therefore a deliberate no-op — a header names a group
// but is not a thing you can be "on".
func (gui *Gui) clickSession(opts gocui.ViewMouseBindingOpts) error {
	if err := gui.cutControlToSessions(); err != nil {
		return err
	}

	index := sessionIndexForRowLine(gui.panelRows(), opts.Y)
	if index < 0 {
		return nil
	}

	if err := gui.applySelection(index); err != nil {
		return err
	}

	if opts.IsDoubleClick {
		return gui.focusSelectedShell()
	}

	return nil
}

// clickHelp is the help popup's click gesture: it is the mouse's equivalent
// of j/k *and* Enter combined, not just navigation — a single click selects
// the row under the pointer and, when that row is actionable, immediately
// runs it. Deliberately not select-then-second-click: unlike a session in
// the list, a help row is a one-shot command rather than something you
// linger on, and the row's dimmed/lit rendering already tells the user in
// advance whether a click will do anything.
//
// opts.Y is a row within the popup's own content coordinates (gocui's
// content-relative Y, same as clickSession's), which is also a helpLine
// index outright — helpLines emits exactly one text line per entry.
func (gui *Gui) clickHelp(opts gocui.ViewMouseBindingOpts) error {
	lines := gui.helpLines()
	if opts.Y < 0 || opts.Y >= len(lines) || !lines[opts.Y].selectable {
		return nil
	}

	for i, lineNum := range selectableHelpLines(lines) {
		if lineNum == opts.Y {
			gui.helpSelectedIndex = i

			break
		}
	}

	if !lines[opts.Y].actionable {
		return gui.renderHelp(gui.g)
	}

	return gui.triggerHelpSelection(gui.g, nil)
}

// clickGroupPicker is clickHelp's counterpart for the "g" key's popup: a
// click selects the row under the pointer and immediately runs it, since
// every row there is actionable (there is no dimmed/unavailable state to
// check first, unlike a help row).
func (gui *Gui) clickGroupPicker(opts gocui.ViewMouseBindingOpts) error {
	sess, ok := gui.sessions.Get(gui.groupPickerSessionID)
	if !ok {
		return nil
	}

	lines := gui.groupPickerLines(sess.Group())
	if opts.Y < 0 || opts.Y >= len(lines) {
		return nil
	}

	gui.groupPickerSelectedIndex = opts.Y

	return gui.triggerGroupPickerSelection(gui.g, nil)
}

// wheelSessions moves the selection rather than scrolling the view. The
// sessions panel has no independent scroll offset — the selection is what the
// view follows — so moving it is the only thing a wheel notch can mean here.
func (gui *Gui) wheelSessions(delta int) func(gocui.ViewMouseBindingOpts) error {
	return func(gocui.ViewMouseBindingOpts) error {
		if err := gui.cutControlToSessions(); err != nil {
			return err
		}

		return gui.selectionMoved(delta)(gui.g, nil)
	}
}

// cutControlToSessions is the mouse's way of landing on the sessions panel:
// gocui dispatches a mouse binding by where the pointer is on screen, not by
// which view currently has focus (view.go's VisibleViewByPosition), so a
// click or a wheel notch over the sessions panel fires here even while the
// output panel is focused.
//
// It deliberately leaves passThroughActive untouched (docs/adr/0011): the
// flag is global and persists across a session-selection change, so browsing
// with the mouse while pass-through is armed for some other session is not a
// reason to disarm it — landing back on the output panel should still be
// interactive. Before ADR 0011 this used to call exitPassThrough() first, as
// a workaround for g.SelFrameColor being a single global attribute: without
// disarming, the pass-through-coloured border would bleed onto the sessions
// panel once it became current. That workaround is gone because the real fix
// is in place — updateBorderColor (input.go) computes the border colour from
// which view is actually current, not from the flag alone, and is wired to
// fire on every focus change via focusManager (layout.go).
func (gui *Gui) cutControlToSessions() error {
	_, err := gui.g.SetCurrentView(sessionsViewName)

	return err
}

// clickOutput focuses the output panel and nothing else. It deliberately does
// not arm pass-through: see rule 3 at the top of this file. A click inside a
// running selection cancels it, the way clicking elsewhere in any editor
// discards the current selection.
func (gui *Gui) clickOutput(opts gocui.ViewMouseBindingOpts) error {
	if gui.forwardMouseToApp(gocui.MouseLeft, gocui.ModNone, opts) {
		return nil
	}

	if _, err := gui.g.SetCurrentView(outputViewName); err != nil {
		return err
	}

	if gui.copyModeActive {
		gui.cancelCopyMode()
	}

	return nil
}

// dragOutput turns a click-and-drag into a copy-mode selection: the first
// motion event anchors it where the button went down, every later one extends
// it. Releasing the button copies nothing — the selection stays on screen and
// "y" yanks it, exactly as it does for a keyboard selection. Writing to the
// clipboard is an action the user asks for, not a side effect of moving a
// pointer.
func (gui *Gui) dragOutput(opts gocui.ViewMouseBindingOpts) error {
	// Copy-mode selects lines of the session's scrollback, which is not what
	// the perf and env tabs are showing — a drag there has nothing to select.
	if gui.outputTab != tabTerminal {
		return nil
	}

	if gui.forwardMouseToApp(gocui.MouseLeft, gocui.ModMotion, opts) {
		return nil
	}

	view, err := gui.g.View(outputViewName)
	if err != nil {
		return nil
	}

	// InnerHeight, not Size: see propagateResize's comment in layout.go.
	_, rows := view.InnerSize()

	if !gui.copyModeActive {
		gui.enterCopyMode()
		// enterCopyMode is a no-op without a session, or on the alternate
		// screen where there is nothing addressable to select.
		if !gui.copyModeActive {
			return nil
		}

		line, ok := gui.outputLineAt(opts.Y)
		if !ok {
			return nil
		}

		gui.copyAnchorLine = line
	}

	line, ok := gui.outputLineAt(opts.Y)
	if !ok {
		return nil
	}

	gui.setCopyCursor(line, rows)

	return nil
}

// wheelOutput scrolls the scrollback by the configured number of lines. delta
// is +1 for "towards older output" so the caller reads the way the gesture
// does; scrollBy's own offset already counts backwards from the live bottom.
func (gui *Gui) wheelOutput(direction int) func(gocui.ViewMouseBindingOpts) error {
	return func(opts gocui.ViewMouseBindingOpts) error {
		key := gocui.MouseWheelUp
		if direction < 0 {
			key = gocui.MouseWheelDown
		}

		if gui.forwardMouseToApp(key, gocui.ModNone, opts) {
			return nil
		}

		gui.scrollBy(direction * gui.wheelLines())

		return nil
	}
}

// wheelLines is the configured notch size, guarding the zero value the way
// tick() guards the refresh interval: several tests build a Gui from a bare
// config.Config{}, and a zero-line wheel would silently do nothing.
func (gui *Gui) wheelLines() int {
	if gui.mouse.WheelLines <= 0 {
		return defaultWheelLines
	}

	return gui.mouse.WheelLines
}

// defaultWheelLines mirrors pkg/config's defaultMouseWheelLines, as this
// package's own fallback — the same duplication every other config-backed
// constant here already follows.
const defaultWheelLines = 3

// appWantsMouse reports whether the program running in the selected session
// has armed a mouse tracking mode, and whether forwarding is allowed at all.
// This — not a guess about which program is running — is what decides who owns
// the mouse: vim with `set mouse=a` asks, htop asks, a shell and an AI agent
// CLI never do.
func (gui *Gui) appWantsMouse() bool {
	if !gui.mouse.Enabled || !gui.mouse.ForwardToApp {
		return false
	}

	// A program only owns the mouse over its own screen. On the perf or env
	// tab the panel is showing a report, and forwarding a click there would
	// send coordinates that mean nothing to the program they land in.
	if gui.outputTab != tabTerminal {
		return false
	}

	sess := gui.selectedSession()
	if sess == nil {
		return false
	}

	mode, _ := sess.Screen().MouseTracking()

	return mode != screen.MouseOff
}

// forwardMouseToApp hands the event to the session's program when that program
// asked for it, and reports whether it did — the caller then leaves the event
// alone. Forwarding only happens while pass-through is armed: outside it the
// panel is being navigated, not typed into, and a click there is lazyshell's.
//
// It is never broadcast to marked sessions, unlike a keystroke: a click is
// about a position on one particular screen, and the same coordinates mean
// something different in every other session.
func (gui *Gui) forwardMouseToApp(key gocui.Key, mod gocui.Modifier, opts gocui.ViewMouseBindingOpts) bool {
	if !gui.passThroughActive || !gui.appWantsMouse() {
		return false
	}

	sess := gui.selectedSession()
	if sess == nil {
		return false
	}

	mode, sgr := sess.Screen().MouseTracking()

	ev := keys.MouseEvent{
		X:      opts.X,
		Y:      opts.Y,
		Press:  true,
		Motion: mod == gocui.ModMotion,
	}

	switch key {
	case gocui.MouseLeft:
		ev.Button = keys.MouseButtonLeft
	case gocui.MouseWheelUp:
		ev.Button = keys.MouseButtonWheelUp
	case gocui.MouseWheelDown:
		ev.Button = keys.MouseButtonWheelDown
	default:
		return false
	}

	if b := keys.EncodeMouse(ev, mode, sgr); b != nil {
		gui.writeToSession(sess, b)
	}

	// Claimed either way: the program owns the mouse right now, so an event it
	// did not want to hear about is dropped rather than handled by lazyshell
	// behind its back.
	return true
}

// outputLineAt converts a row inside the output view into an absolute
// scrollback line, the coordinate space copy-mode and Screen.Find both work
// in. The conversion is a plain addition because the output view never scrolls
// on its own: its origin stays at (0,0) and the emulator decides what is on
// screen, so a view row *is* an emulator row (see output.go's drawCursor).
func (gui *Gui) outputLineAt(row int) (int, bool) {
	sess := gui.selectedSession()
	if sess == nil || row < 0 {
		return 0, false
	}

	return sess.Screen().ScrollbackLen() - gui.getScrollOffset() + row, true
}
