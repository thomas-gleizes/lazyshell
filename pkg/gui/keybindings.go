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
	// Enabled reports whether Handler can meaningfully act right now, given
	// the current session/filter state. nil means always enabled. Only
	// consulted by the help popup (pkg/gui/help.go), which uses it to
	// separate actionable bindings from ones that would currently no-op —
	// setKeybindings itself registers every binding unconditionally, since a
	// key that momentarily does nothing is normal behaviour, not an error.
	Enabled func(*Gui) bool
}

// hasSelectedSession, hasSessions, hasAnySessions and hasActiveFilter are
// Binding.Enabled predicates for the sessionsViewName bindings whose
// Handler no-ops without the state they check — see staticBindings.
func hasSelectedSession(gui *Gui) bool { return gui.selectedSession() != nil }
func hasSessions(gui *Gui) bool        { return len(gui.displaySessions()) > 0 }
func hasAnySessions(gui *Gui) bool     { return len(gui.sessions.List()) > 0 }
func hasActiveFilter(gui *Gui) bool    { return gui.filterActive() }

// hasSelectedGroup gates the four group actions, which all address the group
// through the selection: with nothing selected, or with the selection
// ungrouped, there is no group to act on and the help popup dims them.
func hasSelectedGroup(gui *Gui) bool { return gui.selectedGroup() != "" }

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
		// Directional counterpart to Tab, matching the on-screen geometry: →
		// from the sessions panel goes to the output panel, ← comes back. No
		// Action, so they are not remappable — "cycle_focus" stays the single
		// configurable entry point, these are fixed aliases for it.
		//
		// The ← half cannot live here: it belongs to the output view, which is
		// Editable, so a view-scoped binding would beat the Editor and steal
		// the left arrow from the shell during pass-through. It is matched by
		// hand in editDuringScroll instead, like "zoom" and the tab keys.
		{
			ViewName:    sessionsViewName,
			Key:         gocui.KeyArrowRight,
			Modifier:    gocui.ModNone,
			Handler:     gui.focusOutputPanel,
			Description: gui.tr.T("action.focus_output"),
			Enabled:     hasSelectedSession,
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
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.killSession,
			Description: gui.tr.T("action.kill_session"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "delete_session",
			Key:         'D',
			Modifier:    gocui.ModNone,
			Handler:     gui.deleteSession,
			Description: gui.tr.T("action.delete_session"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "rename_session",
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.renameSession,
			Description: gui.tr.T("action.rename_session"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "duplicate_session",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.duplicateSession,
			Description: gui.tr.T("action.duplicate_session"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "new_named_session",
			Key:         'N',
			Modifier:    gocui.ModNone,
			Handler:     gui.newNamedSession,
			Description: gui.tr.T("action.new_named_session"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "new_session_in_dir",
			Key:         'M',
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
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "zoom",
			Key:         'z',
			Modifier:    gocui.ModNone,
			Handler:     gui.toggleZoom,
			Description: gui.tr.T("action.zoom"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "filter_sessions",
			Key:         '/',
			Modifier:    gocui.ModNone,
			Handler:     gui.showFilter,
			Description: gui.tr.T("action.filter_sessions"),
			Enabled:     hasAnySessions,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "export_session",
			Key:         'w',
			Modifier:    gocui.ModNone,
			Handler:     gui.exportSession,
			Description: gui.tr.T("action.export_session"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "toggle_broadcast",
			Key:         'b',
			Modifier:    gocui.ModNone,
			Handler:     gui.toggleBroadcastMark,
			Description: gui.tr.T("action.toggle_broadcast"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Key:         gocui.KeyEsc,
			Modifier:    gocui.ModNone,
			Handler:     gui.clearFilterKey,
			Description: gui.tr.T("action.clear_filter"),
			Enabled:     hasActiveFilter,
		},
		// The output panel's tab strip (pkg/gui/tabs.go). Scoped to the
		// sessions view, not the output one it acts on: a view-scoped binding
		// beats the Editor (see editOutput's comment), so registering these on
		// outputViewName would make "]" fire during pass-through and stop it
		// ever reaching the shell. The output view reaches them by hand from
		// editDuringScroll instead, the same arrangement "zoom" already uses.
		{
			ViewName:    sessionsViewName,
			Action:      "next_tab",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.nextTab,
			Description: gui.tr.T("action.next_tab"),
		},
		{
			ViewName:    sessionsViewName,
			Action:      "prev_tab",
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.prevTab,
			Description: gui.tr.T("action.prev_tab"),
		},
		// The group actions (ADR 0007). Plain uppercase letters rather than a
		// Ctrl form of each session action's key, which would have read better:
		// keyLabel prints "Ctrl-B" while gocui.Parse only understands "CtrlB",
		// and the README's keybindings example is checked against keyLabel's
		// output *and* run through ValidateConfig — so a Ctrl-keyed remappable
		// action fails the build until that round-trip is fixed.
		{
			ViewName:    sessionsViewName,
			Action:      "set_group",
			Key:         'g',
			Modifier:    gocui.ModNone,
			Handler:     gui.setSessionGroup,
			Description: gui.tr.T("action.set_group"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "filter_group",
			Key:         'G',
			Modifier:    gocui.ModNone,
			Handler:     gui.toggleGroupFilter,
			Description: gui.tr.T("action.filter_group"),
			Enabled:     hasSelectedGroup,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "broadcast_group",
			Key:         'A',
			Modifier:    gocui.ModNone,
			Handler:     gui.toggleGroupBroadcast,
			Description: gui.tr.T("action.broadcast_group"),
			Enabled:     hasSelectedGroup,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "kill_group",
			Key:         'X',
			Modifier:    gocui.ModNone,
			Handler:     gui.killGroup,
			Description: gui.tr.T("action.kill_group"),
			Enabled:     hasSelectedGroup,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "restart_group",
			Key:         'W',
			Modifier:    gocui.ModNone,
			Handler:     gui.restartGroup,
			Description: gui.tr.T("action.restart_group"),
			Enabled:     hasSelectedGroup,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "jump_next_blocked",
			Key:         'B',
			Modifier:    gocui.ModNone,
			Handler:     gui.jumpToNextBlockedSession,
			Description: gui.tr.T("action.jump_next_blocked"),
			Enabled:     hasSessions,
		},
		// Prompt navigation (OSC 133 shell integration). Scoped to
		// sessionsViewName like "zoom" and the tab keys above, for the same
		// reason: these must also work while the output view is focused and
		// scrolling, where it is Editable and SetKeybinding is never
		// consulted — editDuringScroll matches them by hand instead.
		{
			ViewName:    sessionsViewName,
			Action:      "jump_prev_prompt",
			Key:         '{',
			Modifier:    gocui.ModNone,
			Handler:     gui.jumpToPreviousPrompt,
			Description: gui.tr.T("action.jump_prev_prompt"),
			Enabled:     hasSelectedSession,
		},
		{
			ViewName:    sessionsViewName,
			Action:      "jump_next_prompt",
			Key:         '}',
			Modifier:    gocui.ModNone,
			Handler:     gui.jumpToNextPrompt,
			Description: gui.tr.T("action.jump_next_prompt"),
			Enabled:     hasSelectedSession,
		},
		// Also OSC 133 shell integration: copies the last finished command's
		// output without entering copy-mode. Same dual-registration reasoning
		// as the prompt-jump actions above.
		{
			ViewName:    sessionsViewName,
			Action:      "copy_last_output",
			Key:         'Y',
			Modifier:    gocui.ModNone,
			Handler:     gui.copyLastCommandOutputAction,
			Description: gui.tr.T("action.copy_last_output"),
			Enabled:     hasSelectedSession,
		},
		// Global, and declared whether or not --debug was given: bindings() has
		// to be a constant list, since the help popup, knownActions() and the
		// README doc-tests all read it. Enabled is what keeps it out of the
		// help popup when the mode is off, and the handler no-ops there.
		//
		// A global binding with ch == 0 fires even while the output view is
		// Editable, so F12 also works during pass-through — wanted, that is
		// when you most need to get the output panel back. The cost, stated in
		// the README: F12 no longer reaches the session.
		{
			Action:      "toggle_debug",
			Key:         gocui.KeyF12,
			Modifier:    gocui.ModNone,
			Handler:     gui.toggleDebugPanel,
			Description: gui.tr.T("action.toggle_debug"),
			Enabled:     func(gui *Gui) bool { return gui.debug != nil },
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

	// No session to show: the output panel is not a valid focus target, so
	// Tab stays a no-op rather than switching to an empty view.
	if next == outputViewName && gui.selectedSession() == nil {
		return nil
	}

	_, err := g.SetCurrentView(next)

	return err
}

// focusOutputPanel moves focus to the output panel, without arming
// pass-through: → is a navigation gesture like Tab, not "start typing into the
// session" (which is what "i"/Enter are for once there).
func (gui *Gui) focusOutputPanel(g *gocui.Gui, _ *gocui.View) error {
	// Same guard as cycleFocus: with no session the output panel is not a
	// valid focus target.
	if gui.selectedSession() == nil {
		return nil
	}

	_, err := g.SetCurrentView(outputViewName)

	return err
}

// focusSessionsPanel is ←'s handler, called by hand from the output view's
// Editor. While zoomed the sessions view does not exist at all, so there is
// nothing to focus and the key does nothing rather than failing.
func (gui *Gui) focusSessionsPanel() error {
	if gui.zoomed {
		return nil
	}

	return gui.cutControlToSessions()
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

		// This loop is the single place every declared handler is registered,
		// which makes it the one place --debug can instrument all of them at
		// once — see logged.
		name := b.Action
		if name == "" {
			// The fixed alternates (arrows, Ctrl-C, the second key of a pair)
			// carry no action id, so the description is the only thing that
			// identifies them in a log.
			name = b.Description
		}

		handler := gui.logged(name, b.ViewName, keyLabel(key, mod), b.Handler)

		if err := g.SetKeybinding(b.ViewName, key, mod, handler); err != nil {
			return err
		}
	}

	// The output panel's tab strip is a third registry again, purged by
	// SetManager like the other two — so it registers from here for the same
	// reason, not from initView.
	if err := g.SetTabClickBinding(outputViewName, gui.clickOutputTab); err != nil {
		return err
	}

	// Mouse gestures live in their own registry (pkg/gui/mouse.go), which
	// SetManager purges just the same — so they must be registered from here,
	// after it, and not earlier.
	return gui.setMouseBindings(g)
}

func (gui *Gui) quit(*gocui.Gui, *gocui.View) error {
	return gocui.ErrQuit
}
