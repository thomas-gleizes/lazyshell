package gui

import "github.com/jesseduffield/gocui"

// Binding describes a single keybinding. The list is kept flat on purpose
// (lazydocker model); lazygit's per-domain controllers are deliberately not
// used here.
type Binding struct {
	// ViewName is the view the binding applies to, empty for a global binding.
	ViewName string
	// Action is the stable id a config file's keybindings map remaps by
	// (pkg/config's Keybindings). Empty means "not user-remappable" — used
	// for the fixed alternate bindings (arrow keys, Ctrl-C, the second key of
	// a pair) that exist alongside a remappable primary one.
	Action      string
	Key         any
	Modifier    gocui.Modifier
	Handler     func(*gocui.Gui, *gocui.View) error
	Description string
}

// bindings returns every keybinding of the application, in the order
// setKeybindings registers them and the help panel (pkg/gui/help.go) lists
// them.
func (gui *Gui) bindings() []Binding {
	return []Binding{
		{
			ViewName:    "",
			Action:      "quit",
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
			Action:      "cycle_focus",
			Key:         gocui.KeyTab,
			Modifier:    gocui.ModNone,
			Handler:     gui.cycleFocus,
			Description: "Changer de panneau actif",
		},
		{
			ViewName:    "",
			Action:      "help",
			Key:         '?',
			Modifier:    gocui.ModNone,
			Handler:     gui.showHelp,
			Description: "Afficher l'aide",
		},
		{
			ViewName:    sessionsViewName,
			Action:      "select_next",
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
			Action:      "select_prev",
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
			Action:      "new_session",
			Key:         'n',
			Modifier:    gocui.ModNone,
			Handler:     gui.newSession,
			Description: "Nouvelle session",
		},
		{
			ViewName:    sessionsViewName,
			Action:      "kill_session",
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
		{
			ViewName:    sessionsViewName,
			Action:      "rename_session",
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.renameSession,
			Description: "Renommer la session sélectionnée",
		},
		{
			ViewName:    sessionsViewName,
			Action:      "duplicate_session",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.duplicateSession,
			Description: "Dupliquer la session sélectionnée",
		},
		{
			ViewName:    sessionsViewName,
			Action:      "new_session_in_dir",
			Key:         'N',
			Modifier:    gocui.ModNone,
			Handler:     gui.newSessionInDir,
			Description: "Nouvelle session dans un dossier choisi",
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

// resolveBinding applies gui.keymap (pkg/config's Keybindings) to a binding
// with a non-empty Action: a present, parseable entry overrides both the key
// and any modifier it specifies (e.g. "Alt+N"); a missing action, or one that
// fails to parse, keeps the binding's built-in default rather than leaving
// the action unbound.
func (gui *Gui) resolveBinding(b Binding) (any, gocui.Modifier) {
	if b.Action == "" {
		return b.Key, b.Modifier
	}

	spec, ok := gui.keymap[b.Action]
	if !ok {
		return b.Key, b.Modifier
	}

	key, mod, err := gocui.Parse(spec)
	if err != nil {
		return b.Key, b.Modifier
	}

	return key, mod
}

// matchesAction reports whether the given key event is the resolved key for
// action (after any config remap). Used by editDuringScroll for actions that
// must also work while the output view is focused: a global, printable-key
// SetKeybinding never fires as a fallback while the current view is Editable
// (see editDuringScroll's own comment on 'q'), so those actions have to be
// matched and triggered by hand from inside the Editor instead.
func (gui *Gui) matchesAction(action string, key gocui.Key, ch rune) bool {
	for _, b := range gui.bindings() {
		if b.Action != action {
			continue
		}

		resolved, _ := gui.resolveBinding(b)

		switch r := resolved.(type) {
		case rune:
			return ch == r
		case gocui.Key:
			return ch == 0 && key == r
		}
	}

	return false
}

func (gui *Gui) setKeybindings(g *gocui.Gui) error {
	for _, b := range gui.bindings() {
		key, mod := gui.resolveBinding(b)

		if err := g.SetKeybinding(b.ViewName, key, mod, b.Handler); err != nil {
			return err
		}
	}

	return nil
}

func (gui *Gui) quit(*gocui.Gui, *gocui.View) error {
	return gocui.ErrQuit
}
