package gui

import (
	"strings"

	"github.com/rivo/uniseg"
)

// The bottom line of each panel's frame carries that panel's own most-used
// keys, so they are readable at a glance instead of only through the `?` popup.
// This matters most for the output panel: its keys live in editOutput rather
// than in bindings(), so until now they appeared in neither the status bar nor
// the help popup — they were discoverable only by reading the source.
//
// gocui draws View.Footer right-aligned on the frame's bottom line (gui.go's
// drawListFooter), and silently draws *nothing* when the text is wider than the
// view. Hence footerText: hints are added in priority order for as long as they
// fit, so a narrow sessions panel shows the two that matter instead of nothing
// at all.

// footerHint is one "keys:label" group of a panel's footer.
type footerHint struct {
	// actions are binding ids (pkg/config's Keybindings), resolved through the
	// keymap so a remapped key shows the user's key, not the default. Several
	// ids render as one group: select_next+select_prev give "j/k".
	actions []string
	// key is a literal label, for the keys that are not bindings() entries —
	// everything the output view's Editor handles itself.
	key   string
	label string
}

// footerSeparator is a single space: the sessions panel is 30 columns wide by
// default, and anything wider costs a whole hint.
const footerSeparator = " "

// sessionsFooterHints is the sessions panel's keys, most-used first. Order is
// what survives truncation, so it is a priority list, not a catalogue.
func (gui *Gui) sessionsFooterHints() []footerHint {
	return []footerHint{
		{actions: []string{"new_session"}, label: gui.tr.T("footer.new")},
		{actions: []string{"kill_session"}, label: gui.tr.T("footer.kill")},
		{actions: []string{"select_next", "select_prev"}, label: gui.tr.T("footer.navigate")},
		{actions: []string{"rename_session"}, label: gui.tr.T("footer.rename")},
		{actions: []string{"restart_session"}, label: gui.tr.T("footer.restart")},
		{actions: []string{"zoom"}, label: gui.tr.T("footer.zoom")},
		{actions: []string{"duplicate_session"}, label: gui.tr.T("footer.duplicate")},
		{actions: []string{"new_session_in_dir"}, label: gui.tr.T("footer.new_in_dir")},
	}
}

// panelFooter returns the footer for a view at the given inner width, or ""
// when even the first hint does not fit.
func (gui *Gui) panelFooter(viewName string, width int) string {
	switch viewName {
	case sessionsViewName:
		return gui.footerText(gui.sessionsFooterHints(), width)
	case outputViewName:
		return gui.footerText(gui.outputFooterHints(), width)
	}

	return ""
}

// outputFooterHints is the output panel's keys, which depend on what the panel
// is currently doing — this is the one place where a fixed list would be wrong.
func (gui *Gui) outputFooterHints() []footerHint {
	// In pass-through every key but the prefix goes to the shell, so the prefix
	// is the only thing that is true about this panel right now.
	if gui.passThroughActive {
		return []footerHint{{key: prefixName(gui.prefixKey), label: gui.tr.T("footer.exit_passthrough")}}
	}

	hints := []footerHint{{key: "i", label: gui.tr.T("footer.type")}}

	// A full-screen application does not feed the scrollback, so scrolling does
	// nothing (see scrollBy) — advertising it there would be a lie.
	if !gui.selectedIsAltScreen() {
		hints = append(hints,
			footerHint{key: "PgUp/PgDn", label: gui.tr.T("footer.scroll")},
			footerHint{key: "Ctrl-U/D", label: gui.tr.T("footer.half_page")},
		)
	}

	hints = append(hints, footerHint{actions: []string{"zoom"}, label: gui.tr.T("footer.zoom")})

	return hints
}

// footerText joins as many hints as fit in width, in order, and stops at the
// first one that does not — a truncated hint would be worse than a missing one.
func (gui *Gui) footerText(hints []footerHint, width int) string {
	if width <= 0 {
		return ""
	}

	var (
		b     strings.Builder
		shown int
	)

	for _, hint := range hints {
		rendered := gui.renderHint(hint)
		if rendered == "" {
			continue
		}

		// Measured in display columns with the same function gocui itself uses
		// to place the footer, so the two can never disagree about whether it
		// fits: "PgUp/PgDn:défiler" is 18 bytes but 17 columns.
		next := shown + uniseg.StringWidth(rendered)
		if shown > 0 {
			next += uniseg.StringWidth(footerSeparator)
		}

		if next > width {
			break
		}

		if shown > 0 {
			b.WriteString(footerSeparator)
		}
		b.WriteString(rendered)
		shown = next
	}

	return b.String()
}

// renderHint turns one hint into "keys:label", resolving action ids through the
// keymap so a config remap is reflected here too.
func (gui *Gui) renderHint(hint footerHint) string {
	keyPart := hint.key

	if len(hint.actions) > 0 {
		labels := make([]string, 0, len(hint.actions))

		for _, action := range hint.actions {
			if label := gui.actionKeyLabel(action); label != "" {
				labels = append(labels, label)
			}
		}

		if len(labels) == 0 {
			return ""
		}

		keyPart = strings.Join(labels, "/")
	}

	if keyPart == "" {
		return ""
	}

	return keyPart + ":" + hint.label
}

// actionKeyLabel is the human label of whichever key currently triggers action.
func (gui *Gui) actionKeyLabel(action string) string {
	for _, binding := range gui.bindings() {
		if binding.Action != action {
			continue
		}

		key, mod := gui.resolveBinding(binding)

		return keyLabel(key, mod)
	}

	return ""
}
