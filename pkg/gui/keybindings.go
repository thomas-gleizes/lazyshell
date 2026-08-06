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
	b := gui.staticBindings()

	// "1".."9" jump straight to the n-th session. Scoped to sessionsViewName
	// only, never global: the output view's Editor (pkg/gui/input.go's
	// editOutput) intercepts every keystroke before gocui ever consults
	// SetKeybinding, but a global binding on a printable key would still be
	// consulted while a *different*, Editor-less view has focus — there is
	// none of those here, but a global scope would also be wrong in
	// principle, since typing a digit into a pass-through shell must never
	// be mistaken for a jump. Not user-remappable: a digit's meaning is its
	// position, remapping it would defeat the point.
	for i := 1; i <= 9; i++ {
		b = append(b, Binding{
			ViewName:    sessionsViewName,
			Key:         rune('0' + i),
			Modifier:    gocui.ModNone,
			Handler:     gui.selectIndex(i - 1),
			Description: gui.tr.T("action.jump", i),
		})
	}

	return b
}

// staticBindings is every keybinding that is not generated — see bindings.
func (gui *Gui) staticBindings() []Binding {
	return []Binding{
		{
			ViewName:    "",
			Action:      "quit",
			Key:         'q',
			Modifier:    gocui.ModNone,
			Handler:     gui.quit,
			Description: gui.tr.T("action.quit"),
		},
		{
			ViewName:    "",
			Key:         gocui.KeyCtrlC,
			Modifier:    gocui.ModNone,
			Handler:     gui.quit,
			Description: gui.tr.T("action.quit"),
		},
		{
			ViewName:    "",
			Action:      "cycle_focus",
			Key:         gocui.KeyTab,
			Modifier:    gocui.ModNone,
			Handler:     gui.cycleFocus,
			Description: gui.tr.T("action.cycle_focus"),
		},
		{
			ViewName:    "",
			Action:      "help",
			Key:         '?',
			Modifier:    gocui.ModNone,
			Handler:     gui.showHelp,
			Description: gui.tr.T("action.help"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "select_next",
			Key:         'j',
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(1),
			Description: gui.tr.T("action.select_next"),
		},
		{
			ViewName:    sessionsViewName,
			Key:         gocui.KeyArrowDown,
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(1),
			Description: gui.tr.T("action.select_next"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "select_prev",
			Key:         'k',
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(-1),
			Description: gui.tr.T("action.select_prev"),
		},
		{
			ViewName:    sessionsViewName,
			Key:         gocui.KeyArrowUp,
			Modifier:    gocui.ModNone,
			Handler:     gui.selectionMoved(-1),
			Description: gui.tr.T("action.select_prev"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "new_session",
			Key:         'n',
			Modifier:    gocui.ModNone,
			Handler:     gui.newSession,
			Description: gui.tr.T("action.new_session"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "kill_session",
			Key:         'x',
			Modifier:    gocui.ModNone,
			Handler:     gui.killSession,
			Description: gui.tr.T("action.kill_session"),
		},
		{
			ViewName:    sessionsViewName,
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.killSession,
			Description: gui.tr.T("action.kill_session"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "rename_session",
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.renameSession,
			Description: gui.tr.T("action.rename_session"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "duplicate_session",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.duplicateSession,
			Description: gui.tr.T("action.duplicate_session"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "new_session_in_dir",
			Key:         'N',
			Modifier:    gocui.ModNone,
			Handler:     gui.newSessionInDir,
			Description: gui.tr.T("action.new_session_in_dir"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "restart_session",
			Key:         'R',
			Modifier:    gocui.ModNone,
			Handler:     gui.restartSession,
			Description: gui.tr.T("action.restart_session"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "zoom",
			Key:         'z',
			Modifier:    gocui.ModNone,
			Handler:     gui.toggleZoom,
			Description: gui.tr.T("action.zoom"),
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
