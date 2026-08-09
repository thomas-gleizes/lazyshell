package gui

import (
	"fmt"
	"strings"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

const groupPickerViewName = "group-picker"

// groupPickerAction is what Enter (or a click) on a groupPickerLine does.
type groupPickerAction int

const (
	pickExistingGroup groupPickerAction = iota
	pickNoGroup
	pickNewGroup
)

// groupPickerLine is one row of the "g" key's popup.
type groupPickerLine struct {
	text   string
	group  string // meaningful only when action == pickExistingGroup
	action groupPickerAction
}

// groupPickerLines lists every group in use, in the same order the sessions
// panel draws their headers in (groupOrderOf) — picking from the list the eye
// already reads is the whole point, rather than retyping a name that is
// already on screen. "no group" and "new group…" are always offered on top of
// whatever groups exist.
//
// nil when no group exists anywhere yet: a popup holding nothing but "new
// group…" has nothing to pick from, so showGroupPicker skips straight to
// typing one in that case.
func (gui *Gui) groupPickerLines(current string) []groupPickerLine {
	groups := groupOrderOf(gui.sessions.List(), gui.groupOrder)
	if len(groups) == 0 {
		return nil
	}

	lines := make([]groupPickerLine, 0, len(groups)+2)

	for _, group := range groups {
		lines = append(lines, groupPickerLine{text: pickerLabel(group, group == current), group: group, action: pickExistingGroup})
	}

	lines = append(lines, groupPickerLine{
		text:   pickerLabel(gui.tr.T("group_picker.none"), current == ""),
		action: pickNoGroup,
	})
	lines = append(lines, groupPickerLine{text: gui.tr.T("group_picker.new"), action: pickNewGroup})

	return lines
}

// pickerLabel prefixes a row's text with a checkmark when it is the
// session's current group — context, not a third selection state: picking
// the marked row again is a harmless no-op.
func pickerLabel(text string, isCurrent bool) string {
	if isCurrent {
		return "✓ " + text
	}

	return "  " + text
}

// showGroupPicker opens the popup on sess, or — the empty-list case — goes
// straight to the "new group" prompt: the first group anyone assigns in a
// fresh lazyshell has nothing to be picked from yet.
func (gui *Gui) showGroupPicker(sess *session.Session) error {
	if gui.groupPickerLines(sess.Group()) == nil {
		return gui.showNewGroupPrompt(sess)
	}

	gui.groupPickerSessionID = sess.ID
	gui.groupPickerSelectedIndex = 0

	if current := gui.g.CurrentView(); current != nil {
		gui.groupPickerReturnView = current.Name()
	}

	return gui.renderGroupPicker(gui.g)
}

// renderGroupPicker (re)draws the popup: sized and centered from its content
// on first creation, same popup shape as help.go's renderHelp, then rewritten
// and re-focused on every call — moveGroupPickerSelection and
// clickGroupPicker both just call this again after moving the selection.
//
// Closes itself if the session it was opened on is gone (killed from another
// path while the popup was up) or ended up with nothing to pick — the same
// defensive read renderHelp does not need, because bindings() never shrinks
// out from under it the way a session list can.
func (gui *Gui) renderGroupPicker(g *gocui.Gui) error {
	sess, ok := gui.sessions.Get(gui.groupPickerSessionID)
	if !ok {
		return gui.closeGroupPicker()
	}

	lines := gui.groupPickerLines(sess.Group())
	if len(lines) == 0 {
		return gui.closeGroupPicker()
	}

	if gui.groupPickerSelectedIndex >= len(lines) {
		gui.groupPickerSelectedIndex = len(lines) - 1
	}
	if gui.groupPickerSelectedIndex < 0 {
		gui.groupPickerSelectedIndex = 0
	}

	var b strings.Builder

	width := 0

	for _, line := range lines {
		fmt.Fprintln(&b, line.text)

		if n := len([]rune(line.text)); n > width {
			width = n
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

	view, err := g.SetView(groupPickerViewName, x0, y0, x0+width, y0+height, 0)
	if err != nil {
		if !goerrors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		view.Title = gui.tr.T("group_picker.title")
		view.Highlight = true
		view.HighlightInactive = true
		view.SelBgColor = gui.theme.SelectedBgColor

		for _, key := range []any{'j', gocui.KeyArrowDown} {
			if err := g.SetKeybinding(groupPickerViewName, key, gocui.ModNone, gui.moveGroupPickerSelection(1)); err != nil {
				return err
			}
		}

		for _, key := range []any{'k', gocui.KeyArrowUp} {
			if err := g.SetKeybinding(groupPickerViewName, key, gocui.ModNone, gui.moveGroupPickerSelection(-1)); err != nil {
				return err
			}
		}

		if err := g.SetKeybinding(groupPickerViewName, gocui.KeyEnter, gocui.ModNone, gui.triggerGroupPickerSelection); err != nil {
			return err
		}

		if err := g.SetKeybinding(groupPickerViewName, gocui.KeyEsc, gocui.ModNone, gui.closeGroupPickerKey); err != nil {
			return err
		}
	}

	view.Clear()
	fmt.Fprint(view, b.String())
	view.FocusPoint(0, gui.groupPickerSelectedIndex, true)

	_, err = g.SetCurrentView(groupPickerViewName)

	return err
}

// moveGroupPickerSelection moves the popup's selection by delta (j/k,
// arrows), clamped to the list's bounds by renderGroupPicker.
func (gui *Gui) moveGroupPickerSelection(delta int) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, _ *gocui.View) error {
		gui.groupPickerSelectedIndex += delta

		return gui.renderGroupPicker(g)
	}
}

// triggerGroupPickerSelection is Enter on the popup: closes it, then acts on
// whichever row was highlighted — every row here is actionable, unlike
// help.go's, so there is no dimmed/inert state to check first.
func (gui *Gui) triggerGroupPickerSelection(g *gocui.Gui, _ *gocui.View) error {
	sess, ok := gui.sessions.Get(gui.groupPickerSessionID)
	if !ok {
		return gui.closeGroupPicker()
	}

	lines := gui.groupPickerLines(sess.Group())
	if gui.groupPickerSelectedIndex >= len(lines) {
		return gui.closeGroupPicker()
	}

	line := lines[gui.groupPickerSelectedIndex]

	if err := gui.closeGroupPicker(); err != nil {
		return err
	}

	switch line.action {
	case pickNewGroup:
		return gui.showNewGroupPrompt(sess)
	case pickNoGroup:
		return gui.applySessionGroup(sess, "")
	default:
		return gui.applySessionGroup(sess, line.group)
	}
}

// showNewGroupPrompt is the picker's "new group…" row and the empty-list
// fallback both: the plain text-entry popup, empty rather than pre-filled —
// unlike the old single-prompt "g", reaching this means the user already
// said no to every existing group and to "no group", so there is nothing
// useful to pre-fill.
func (gui *Gui) showNewGroupPrompt(sess *session.Session) error {
	return gui.showPrompt(gui.tr.T("prompt.new_group"), "", func(group string) error {
		return gui.applySessionGroup(sess, group)
	})
}

// closeGroupPickerKey adapts closeGroupPicker to the Handler signature for
// the popup's dismiss key (Esc).
func (gui *Gui) closeGroupPickerKey(*gocui.Gui, *gocui.View) error {
	return gui.closeGroupPicker()
}

// closeGroupPicker tears the popup down and restores focus to whichever view
// was current before showGroupPicker opened it.
func (gui *Gui) closeGroupPicker() error {
	gui.g.DeleteViewKeybindings(groupPickerViewName)

	if err := gui.g.DeleteView(groupPickerViewName); err != nil {
		return err
	}

	returnView := gui.groupPickerReturnView
	if returnView == "" {
		returnView = sessionsViewName
	}

	_, err := gui.g.SetCurrentView(returnView)

	return err
}
