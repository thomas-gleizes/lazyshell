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
		{
			ViewName:    "",
			Key:         gocui.KeyTab,
			Modifier:    gocui.ModNone,
			Handler:     gui.cycleFocus,
			Description: "Changer de panneau actif",
		},
		{
			ViewName:    sessionsViewName,
			Key:         'j',
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(1),
			Description: "Session suivante",
		},
		{
			ViewName:    sessionsViewName,
			Key:         gocui.KeyArrowDown,
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(1),
			Description: "Session suivante",
		},
		{
			ViewName:    sessionsViewName,
			Key:         'k',
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(-1),
			Description: "Session précédente",
		},
		{
			ViewName:    sessionsViewName,
			Key:         gocui.KeyArrowUp,
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(-1),
			Description: "Session précédente",
		},
		{
			ViewName:    sessionsViewName,
			Key:         'n',
			Modifier:    gocui.ModNone,
			Handler:     gui.newSession,
			Description: "Nouvelle session",
		},
		{
			ViewName:    sessionsViewName,
			Key:         'x',
			Modifier:    gocui.ModNone,
			Handler:     gui.killSession,
			Description: "Tuer la session sélectionnée",
		},
		{
			ViewName:    sessionsViewName,
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.killSession,
			Description: "Tuer la session sélectionnée",
		},
	}
}

// focusOrder is the sequence Tab cycles through.
var focusOrder = []string{sessionsViewName, outputViewName}

// cycleFocus moves the current view to the next one in focusOrder.
func (gui *Gui) cycleFocus(g *gocui.Gui, _ *gocui.View) error {
	name := ""
	if current := g.CurrentView(); current != nil {
		name = current.Name()
	}

	next := focusOrder[0]
	for i, n := range focusOrder {
		if n == name {
			next = focusOrder[(i+1)%len(focusOrder)]

			break
		}
	}

	_, err := g.SetCurrentView(next)

	return err
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
