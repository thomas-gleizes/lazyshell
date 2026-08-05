package gui

import "github.com/jesseduffield/gocui"

// focusManager is a second gocui.Manager, run alongside the layout manager on
// every redraw. gocui itself has no OnFocus/OnFocusLost event — only
// g.CurrentView(), the state right now — so this is the only way to
// synthesize "focus changed" by comparing it to what it was on the previous
// redraw.
type focusManager struct {
	last string

	onFocus     map[string]func()
	onFocusLost map[string]func()
}

func newFocusManager() *focusManager {
	return &focusManager{
		onFocus:     make(map[string]func()),
		onFocusLost: make(map[string]func()),
	}
}

// Layout implements gocui.Manager. Despite the name, it does not touch any
// view: it only detects a focus change and fires the matching hooks.
func (f *focusManager) Layout(g *gocui.Gui) error {
	name := ""
	if current := g.CurrentView(); current != nil {
		name = current.Name()
	}

	if name == f.last {
		return nil
	}

	if h, ok := f.onFocusLost[f.last]; ok {
		h()
	}

	f.last = name

	if h, ok := f.onFocus[name]; ok {
		h()
	}

	return nil
}
