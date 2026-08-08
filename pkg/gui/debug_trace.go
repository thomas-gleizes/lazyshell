package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/keys"
)

// This file holds the --debug instrumentation that is *shared*: the handler
// wrappers and the key formatting. The one-line gui.debug.Action / .Event
// calls live at their own call sites, where they can name the branch they are
// in — an indirection would only hide which case actually fired, which is the
// single thing the log exists to answer.

// logged wraps a keybinding handler so the debug log records that it fired,
// and what it returned. It returns the handler untouched when the debug mode
// is off, so a normal run pays nothing at all — not even a closure per
// keystroke.
//
// Called from setKeybindings' registration loop, which is the one place every
// declared Binding.Handler passes through.
func (gui *Gui) logged(action, viewName, key string, handler func(*gocui.Gui, *gocui.View) error) func(*gocui.Gui, *gocui.View) error {
	if gui.debug == nil || handler == nil {
		return handler
	}

	if viewName == "" {
		viewName = "global"
	}

	return func(g *gocui.Gui, v *gocui.View) error {
		gui.debug.Action("%s [%s on %s]", action, key, viewName)

		err := handler(g, v)
		if err != nil {
			gui.debug.Action("%s → error: %v", action, err)
		}

		return err
	}
}

// loggedMouse is logged's counterpart for the mouse registry, which gocui
// gives a different handler shape. The coordinates are logged too: a click
// that lands one row off is the usual reason to be looking.
func (gui *Gui) loggedMouse(binding *gocui.ViewMouseBinding) *gocui.ViewMouseBinding {
	if gui.debug == nil || binding == nil || binding.Handler == nil {
		return binding
	}

	inner := binding.Handler
	name := mouseKeyName(binding.Key, binding.Modifier)
	view := binding.ViewName

	// A copy, not a mutation: mouseBindings() rebuilds its slice on every
	// call, but nothing says a future caller will, and a binding that
	// accumulated a wrapper per registration would log the same click twice.
	wrapped := *binding
	wrapped.Handler = func(opts gocui.ViewMouseBindingOpts) error {
		gui.debug.Action("mouse %s on %s at (%d,%d)", name, view, opts.X, opts.Y)

		err := inner(opts)
		if err != nil {
			gui.debug.Action("mouse %s → error: %v", name, err)
		}

		return err
	}

	return &wrapped
}

// mouseKeyName labels the handful of mouse keys mouseBindings() uses. Separate
// from keyLabel because those values collide with the Shift-arrows (ADR 0003)
// and must never be rendered as such here.
func mouseKeyName(key gocui.Key, mod gocui.Modifier) string {
	var name string

	switch key {
	case gocui.MouseLeft:
		name = "left"
	case gocui.MouseWheelUp:
		name = "wheel-up"
	case gocui.MouseWheelDown:
		name = "wheel-down"
	default:
		name = fmt.Sprintf("key(%d)", key)
	}

	if mod&gocui.ModMotion != 0 {
		name += "+drag"
	}

	return name
}

// debugKeyName renders a raw key event the way a user would name it — "Ctrl-O",
// "Alt+j", "↓", "a" — for the key log.
//
// It cannot reuse keyLabel: that one takes an `any` holding either a rune or a
// gocui.Key, because a Binding declares one or the other. A live event always
// carries both, and which of the two is meaningful is decided by ch == 0.
func debugKeyName(key gocui.Key, ch rune, mod gocui.Modifier) string {
	var label string

	switch {
	case ch != 0:
		label = keyLabel(ch, gocui.ModNone)
	default:
		label = keyLabel(key, gocui.ModNone)
	}

	if mod&gocui.ModAlt != 0 {
		label = "Alt+" + label
	}

	if mod&gocui.ModMotion != 0 {
		label += "+motion"
	}

	return label
}

// logKeyEvent records one keystroke as the output panel's Editor sees it,
// before and after keys.Normalize — the two are logged together because the
// interesting failures are exactly the ones where they disagree (a Ctrl
// modifier folded into a control key, a terminal that sends a modifier
// lazyshell then drops).
func (gui *Gui) logKeyEvent(rawKey gocui.Key, rawCh rune, rawMod gocui.Modifier) {
	if gui.debug == nil {
		return
	}

	key, ch, mod := keys.Normalize(rawKey, rawCh, rawMod)

	line := fmt.Sprintf("%s (key=%d ch=%d mod=%d)", debugKeyName(rawKey, rawCh, rawMod), rawKey, rawCh, rawMod)

	if key != rawKey || ch != rawCh || mod != rawMod {
		line += fmt.Sprintf(" → %s (key=%d ch=%d mod=%d)", debugKeyName(key, ch, mod), key, ch, mod)
	}

	gui.debug.Key("%s [%s]", line, gui.inputModeName())
}

// inputModeName is which of the output panel's modes the keystroke landed in,
// which is what decides where it went. Listed in the order editOutput and
// editDuringScroll actually test them.
func (gui *Gui) inputModeName() string {
	switch {
	case gui.passThroughActive:
		return "pass-through"
	case gui.outputTab != tabTerminal:
		return "tab:" + gui.outputTab.String()
	case gui.copyModeActive:
		return "copy-mode"
	case gui.searchActive():
		return "search"
	default:
		return "scroll"
	}
}
