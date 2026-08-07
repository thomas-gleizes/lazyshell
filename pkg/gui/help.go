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

// helpContent renders one line per binding as "<key>  <description>",
// resolved through gui.resolveBinding so a config-driven remap shows up here
// too. A pure function, kept separate from the gocui-writing side so it can
// be tested directly, the same way sessionsPanelContent is.
func (gui *Gui) helpContent() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s — lazyshell %s\n", gui.tr.T("help.header"), version.Version)
	fmt.Fprintln(&b)

	for _, binding := range gui.bindings() {
		key, mod := gui.resolveBinding(binding)
		fmt.Fprintf(&b, "  %-10s %s\n", keyLabel(key, mod), binding.Description)
	}

	// The mouse gestures are listed as prose rather than in the key column
	// above: they live in gocui's other registry (see mouse.go), they are not
	// remappable, and "click" is not a key spec keyLabel could render. Hidden
	// entirely when the mouse is off, so the help never advertises a gesture
	// that does nothing.
	if gui.mouse.Enabled {
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "  %s\n", gui.tr.T("help.mouse_header"))

		for _, key := range []string{
			"help.mouse_click",
			"help.mouse_double_click",
			"help.mouse_wheel",
			"help.mouse_drag",
		} {
			fmt.Fprintf(&b, "    %s\n", gui.tr.T(key))
		}
	}

	return b.String()
}

// showHelp opens a popup listing every keybinding's description, sized to
// its content and centered — same popup shape as confirm.go's showConfirm,
// but read-only and dismissed by any key rather than a fixed y/n/Esc set.
func (gui *Gui) showHelp(g *gocui.Gui, _ *gocui.View) error {
	if current := g.CurrentView(); current != nil {
		gui.helpReturnView = current.Name()
	}

	content := gui.helpContent()
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")

	width := 0
	for _, line := range lines {
		if len(line) > width {
			width = len(line)
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
		// Editable+Editor, not SetKeybinding: the point is that *any* key
		// closes this popup, and gocui has no "catch-all" keybinding — an
		// Editor sees every keystroke instead. Mirrors output.go's
		// editOutput, which is the same trick for the same reason.
		view.Editable = true
		view.Editor = gocui.EditorFunc(gui.closeHelpOnAnyKey)
	}

	view.Clear()
	fmt.Fprint(view, content)

	_, err = g.SetCurrentView(helpViewName)

	return err
}

// closeHelpOnAnyKey is the help popup's Editor: every keystroke, without
// exception, dismisses it.
func (gui *Gui) closeHelpOnAnyKey(*gocui.View, gocui.Key, rune, gocui.Modifier) bool {
	_ = gui.closeHelp()

	return true
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
