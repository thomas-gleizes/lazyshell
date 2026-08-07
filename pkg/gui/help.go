package gui

import (
	"fmt"
	"strings"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/version"
)

const helpViewName = "help"

// keyDisplayNames renders the non-printable keys used in bindings() as short
// human labels for the help popup. Only covers what bindings() actually uses
// plus Ctrl-A..Z (reachable through config-driven remaps) — this is a display
// helper, not a general key-naming table like pkg/keys'.
var keyDisplayNames = map[gocui.Key]string{
	gocui.KeyCtrlC:     "Ctrl-C",
	gocui.KeyTab:       "Tab",
	gocui.KeyArrowUp:   "↑",
	gocui.KeyArrowDown: "↓",
}

// keyLabel renders a Binding's resolved key/modifier as a short human label,
// e.g. "n", "Tab", "Ctrl-C", "Alt+n".
func keyLabel(key any, mod gocui.Modifier) string {
	var label string

	switch k := key.(type) {
	case rune:
		label = string(k)
	case gocui.Key:
		switch {
		case keyDisplayNames[k] != "":
			label = keyDisplayNames[k]
		case k >= gocui.KeyCtrlA && k <= gocui.KeyCtrlZ:
			label = fmt.Sprintf("Ctrl-%c", 'A'+(k-gocui.KeyCtrlA))
		default:
			label = fmt.Sprintf("key(%d)", k)
		}
	default:
		label = fmt.Sprintf("%v", key)
	}

	if mod&gocui.ModAlt != 0 {
		label = "Alt+" + label
	}

	return label
}

// helpLine is one row of the help popup. A section header or blank
// separator has selectable == false and carries only text; a binding row has
// selectable == true, its resolved Binding, and whether Binding.Enabled
// allowed it to run given the current session/filter state.
type helpLine struct {
	text       string
	binding    Binding
	selectable bool
	actionable bool
}

// helpLines renders every binding as a row and splits them into an
// "available" and an "unavailable" group (mirrors Binding.Enabled), each
// group kept contiguous rather than interleaved in bindings()'s own order —
// that grouping is what selectableHelpLines/currentHelpLine walk to decide
// what j/k, Enter and a click can do. Mouse gestures are appended as plain,
// non-selectable prose: they are not remappable, "click" is not a key spec
// keyLabel could render, and they live in gocui's other registry (see
// mouse.go). Hidden entirely when the mouse is off.
func (gui *Gui) helpLines() []helpLine {
	lines := []helpLine{
		{text: fmt.Sprintf("%s — lazyshell %s", gui.tr.T("help.header"), version.Version)},
		{text: ""},
	}

	var available, unavailable []helpLine

	for _, b := range gui.bindings() {
		key, mod := gui.resolveBinding(b)

		line := helpLine{
			text:       fmt.Sprintf("  %-10s %s", keyLabel(key, mod), b.Description),
			binding:    b,
			selectable: true,
			actionable: b.Enabled == nil || b.Enabled(gui),
		}

		if line.actionable {
			available = append(available, line)
		} else {
			unavailable = append(unavailable, line)
		}
	}

	lines = append(lines, helpLine{text: gui.tr.T("help.actions_available")})
	lines = append(lines, available...)

	if len(unavailable) > 0 {
		lines = append(lines, helpLine{text: ""}, helpLine{text: gui.tr.T("help.actions_unavailable")})
		lines = append(lines, unavailable...)
	}

	if gui.mouse.Enabled {
		lines = append(lines, helpLine{text: ""}, helpLine{text: gui.tr.T("help.mouse_header")})

		for _, key := range []string{
			"help.mouse_click",
			"help.mouse_double_click",
			"help.mouse_wheel",
			"help.mouse_drag",
		} {
			lines = append(lines, helpLine{text: "    " + gui.tr.T(key)})
		}
	}

	return lines
}

// renderHelpContent turns lines into the popup's body text, dimming
// unavailable bindings with a raw ANSI SGR "faint" code and reset — the same
// trick sessions_panel.go's colorizeMarker uses for the agent-state marker,
// so this stays a plain-text popup rather than needing a second Theme color.
func renderHelpContent(lines []helpLine) string {
	var b strings.Builder

	for _, line := range lines {
		text := line.text
		if line.selectable && !line.actionable {
			text = "\x1b[2m" + text + "\x1b[0m"
		}

		fmt.Fprintln(&b, text)
	}

	return b.String()
}

// helpContent renders gui.helpLines() — kept as its own method (rather than
// inlined into renderHelp) so tests can assert on it directly, the same way
// sessionsPanelContent is tested apart from the view it gets written into.
func (gui *Gui) helpContent() string {
	return renderHelpContent(gui.helpLines())
}

// selectableHelpLines returns the indices into lines that Enter or a click
// can act on — everything else is a header or separator. Each helpLine is
// exactly one rendered text line, so an index here is also the row number
// FocusPoint and a click's opts.Y need.
func selectableHelpLines(lines []helpLine) []int {
	var idx []int

	for i, l := range lines {
		if l.selectable {
			idx = append(idx, i)
		}
	}

	return idx
}

// currentHelpLine clamps gui.helpSelectedIndex into lines' selectable rows
// and returns the row it lands on plus its line number, mirroring
// selectedSession's clamp-on-read: a session exiting or a filter changing
// can move rows between the available/unavailable groups or shrink the list
// out from under a selection that was pointing past the end.
func (gui *Gui) currentHelpLine(lines []helpLine) (helpLine, int, bool) {
	idx := selectableHelpLines(lines)
	if len(idx) == 0 {
		return helpLine{}, -1, false
	}

	sel := gui.helpSelectedIndex
	if sel < 0 {
		sel = 0
	}
	if sel >= len(idx) {
		sel = len(idx) - 1
	}
	gui.helpSelectedIndex = sel

	return lines[idx[sel]], idx[sel], true
}

// showHelp opens the help popup, or refreshes it in place if it is already
// open (the global "?" binding re-fires it while it has focus, since the
// popup does not shadow it). helpReturnView and the selection are only reset
// on a fresh open, not on every refresh triggered by j/k.
func (gui *Gui) showHelp(g *gocui.Gui, _ *gocui.View) error {
	if _, err := g.View(helpViewName); err != nil {
		if current := g.CurrentView(); current != nil {
			gui.helpReturnView = current.Name()
		}

		gui.helpSelectedIndex = 0
	}

	return gui.renderHelp(g)
}

// renderHelp (re)draws the popup: sized and centered from its content on
// first creation, same popup shape as confirm.go's showConfirm, then
// rewritten and re-focused on every call — moveHelpSelection and clickHelp
// both just call this again after changing gui.helpSelectedIndex.
func (gui *Gui) renderHelp(g *gocui.Gui) error {
	lines := gui.helpLines()
	content := renderHelpContent(lines)

	width := 0
	for _, line := range lines {
		if len(line.text) > width {
			width = len(line.text)
		}
	}
	width += 4
	height := len(lines) + 2

	maxX, maxY := g.Size()
	if width > maxX-2 {
		width = maxX - 2
	}
	if height > maxY-2 {
		height = maxY - 2
	}

	x0 := (maxX - width) / 2
	y0 := (maxY - height) / 2

	view, err := g.SetView(helpViewName, x0, y0, x0+width, y0+height, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = gui.tr.T("help.title")
		view.Highlight = true
		view.HighlightInactive = true
		view.SelBgColor = gui.theme.SelectedBgColor

		for _, key := range []any{'j', gocui.KeyArrowDown} {
			if err := g.SetKeybinding(helpViewName, key, gocui.ModNone, gui.moveHelpSelection(1)); err != nil {
				return err
			}
		}
		for _, key := range []any{'k', gocui.KeyArrowUp} {
			if err := g.SetKeybinding(helpViewName, key, gocui.ModNone, gui.moveHelpSelection(-1)); err != nil {
				return err
			}
		}
		if err := g.SetKeybinding(helpViewName, gocui.KeyEnter, gocui.ModNone, gui.triggerHelpSelection); err != nil {
			return err
		}
		for _, key := range []any{gocui.KeyEsc, '?', 'q'} {
			if err := g.SetKeybinding(helpViewName, key, gocui.ModNone, gui.closeHelpKey); err != nil {
				return err
			}
		}
	}

	view.Clear()
	fmt.Fprint(view, content)

	if _, lineNum, ok := gui.currentHelpLine(lines); ok {
		view.FocusPoint(0, lineNum, true)
	}

	_, err = g.SetCurrentView(helpViewName)

	return err
}

// moveHelpSelection moves the help popup's selection by delta (j/k, arrows),
// clamped to the selectable rows by currentHelpLine inside renderHelp.
func (gui *Gui) moveHelpSelection(delta int) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, _ *gocui.View) error {
		gui.helpSelectedIndex += delta

		return gui.renderHelp(g)
	}
}

// triggerHelpSelection is Enter on the help popup: it runs the selected
// binding's Handler if the row is actionable, and does nothing otherwise —
// an unavailable row is inert, not an error, the same way pressing its real
// key on the sessions panel would already silently no-op.
//
// The popup is closed *before* the handler runs, not after: closeHelp
// restores focus to helpReturnView, and every Handler in bindings() acts
// on gui's own state or the *gocui.Gui passed in rather than its *gocui.View
// argument (see keybindings.go's setKeybindings, which never reads it
// either), so running the handler against the now-current view is what
// makes it behave exactly as if its real key had been pressed there.
func (gui *Gui) triggerHelpSelection(g *gocui.Gui, _ *gocui.View) error {
	lines := gui.helpLines()

	line, _, ok := gui.currentHelpLine(lines)
	if !ok || !line.actionable {
		return nil
	}

	handler := line.binding.Handler

	if err := gui.closeHelp(); err != nil {
		return err
	}

	return handler(g, g.CurrentView())
}

// closeHelpKey adapts closeHelp to the Handler signature for the popup's
// dismiss keys (Esc, "?", "q").
func (gui *Gui) closeHelpKey(*gocui.Gui, *gocui.View) error {
	return gui.closeHelp()
}

// closeHelp tears the popup down and restores focus to whichever view was
// current before showHelp opened it.
func (gui *Gui) closeHelp() error {
	gui.g.DeleteViewKeybindings(helpViewName)

	if err := gui.g.DeleteView(helpViewName); err != nil {
		return err
	}

	returnView := gui.helpReturnView
	if returnView == "" {
		returnView = sessionsViewName
	}

	_, err := gui.g.SetCurrentView(returnView)

	return err
}
