package gui

import "github.com/jesseduffield/gocui"

// Binding describes a single keybinding. The list is kept flat on purpose
// (lazydocker model); lazygit's per-domain controllers are deliberately not
// used here.
type Binding struct {
	// ViewName is the view the binding applies to, empty for a global binding.
	ViewName    string
	Key         any
	Modifier    gocui.Modifier
	Handler     func(*gocui.Gui, *gocui.View) error
	Description string
}

// bindings returns every keybinding of the application. Description feeds the
// help panel added in phase 5.
func (gui *Gui) bindings() []Binding {
	return []Binding{
		{
			ViewName:    "",
			Key:         'q',
			Modifier:    gocui.ModNone,
			Handler:     gui.quit,
			Description: "Quitter lazyshell",
		},
		{
			ViewName:    "",
			Key:         gocui.KeyCtrlC,
			Modifier:    gocui.ModNone,
			Handler:     gui.quit,
			Description: "Quitter lazyshell",
		},
	}
}

func (gui *Gui) setKeybindings(g *gocui.Gui) error {
	for _, b := range gui.bindings() {
		if err := g.SetKeybinding(b.ViewName, b.Key, b.Modifier, b.Handler); err != nil {
			return err
		}
	}

	return nil
}

func (gui *Gui) quit(*gocui.Gui, *gocui.View) error {
	return gocui.ErrQuit
}
