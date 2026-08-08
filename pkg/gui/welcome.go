package gui

import (
	"fmt"
	"strings"

	"github.com/jesseduffield/gocui"
	"github.com/rivo/uniseg"

	"github.com/thomas-gleizes/lazyshell/pkg/version"
)

// The output panel is a view *of a session*, and there is not always one: the
// last session can be killed and deleted, and a filter can hide every session
// there is. Before this, neither case had an empty state — showOutput was
// simply not called, so the render task of the session that had just been
// deleted kept running and kept pushing its last screen. The panel showed the
// logs of something that no longer existed, indefinitely.
//
// What replaces them is a welcome screen: the application's name and version,
// what state the interface is in, and the handful of keys that do anything from
// here. The keys are read back out of bindings() rather than written down,
// because a user remap (pkg/config's Keybindings) has to be reflected here for
// the screen to be worth showing at all — the same rule footer.go's
// actionKeyLabel already follows.

// welcomeActions are the binding ids the welcome screen advertises, in display
// order. Deliberately short: this is the list of things that can be done with
// no session, not a second copy of the help popup — which is itself one of the
// four entries.
var welcomeActions = []string{"new_session", "new_named_session", "help", "quit"}

// welcomeHint is one advertised key: its resolved label and what it does, both
// already translated by the time they get here.
type welcomeHint struct {
	key   string
	label string
}

// welcomeHints resolves welcomeActions against the current keymap, skipping any
// id bindings() does not carry — knownActions() keeps the two in step, but a
// missing entry should cost one line of the screen, not panic it.
func (gui *Gui) welcomeHints() []welcomeHint {
	hints := make([]welcomeHint, 0, len(welcomeActions))

	for _, action := range welcomeActions {
		for _, binding := range gui.bindings() {
			if binding.Action != action {
				continue
			}

			key, mod := gui.resolveBinding(binding)
			hints = append(hints, welcomeHint{key: keyLabel(key, mod), label: binding.Description})

			break
		}
	}

	return hints
}

// welcomeSubtitle says which of the two empty states this is. They look the
// same to selectedSession() — nil either way — but not to the user: an active
// filter hiding everything is a state to *leave*, and saying "no sessions"
// there would be a plain lie about sessions that are still running.
func (gui *Gui) welcomeSubtitle() string {
	if len(gui.sessions.List()) > 0 {
		return gui.tr.T("welcome.filtered")
	}

	return gui.tr.T("welcome.empty")
}

// welcomeContent lays the screen out for a panel of this inner size: the block
// is centered as a block (vertically and horizontally), with its lines left
// aligned inside it, so the key column stays a column.
//
// A pure function, kept apart from the view it gets written into for the same
// reason sessionsPanelContent is: it can then be asserted on directly, with no
// gocui in the test.
func welcomeContent(subtitle string, hints []welcomeHint, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	keyWidth := 0
	for _, hint := range hints {
		if w := uniseg.StringWidth(hint.key); w > keyWidth {
			keyWidth = w
		}
	}

	lines := []string{
		fmt.Sprintf("lazyshell %s", version.Version),
		"",
		subtitle,
	}

	if len(hints) > 0 {
		lines = append(lines, "")

		for _, hint := range hints {
			// Padded by hand rather than with %-*s: a width verb counts bytes,
			// and these labels are display columns (an "↑" is three bytes wide
			// and one column).
			pad := strings.Repeat(" ", keyWidth-uniseg.StringWidth(hint.key))
			lines = append(lines, fmt.Sprintf("%s%s   %s", hint.key, pad, hint.label))
		}
	}

	blockWidth := 0
	for _, line := range lines {
		if w := uniseg.StringWidth(line); w > blockWidth {
			blockWidth = w
		}
	}

	indent := (width - blockWidth) / 2
	if indent < 0 {
		indent = 0
	}

	var b strings.Builder

	// Vertical centering is best-effort: a panel shorter than the block gets
	// the block from its first line, clipped by the view (Wrap is off), rather
	// than a negative offset that would hide the name and version first.
	for i := 0; i < (height-len(lines))/2; i++ {
		b.WriteString("\n")
	}

	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}

		if line != "" {
			b.WriteString(truncateToWidth(strings.Repeat(" ", indent)+line, width))
		}
	}

	return b.String()
}

// showWelcome is what onSelectionChanged and restartOutput call in place of
// showOutput when there is nothing to show. Stopping the task first is the
// load-bearing half: without it the previous session's render loop keeps
// pushing frames over whatever is written here, which is exactly the bug this
// screen exists to fix.
//
// Called from gocui's own goroutine (a keybinding handler), like showOutput, so
// it writes the view directly rather than through g.Update.
func (gui *Gui) showWelcome() {
	gui.outputTasks.Stop()

	if gui.g == nil {
		return
	}

	// The terminal cursor belongs to a session's emulated screen; there is
	// none, so it must not stay parked wherever the last frame drew it.
	gui.g.Cursor = false

	view, err := gui.g.View(outputViewName)
	if err != nil {
		// Not laid out yet — layout renders the welcome screen itself on every
		// pass while the panel is empty, so the next frame covers this.
		return
	}

	gui.renderWelcome(view)
}

// renderWelcome writes the screen into the output view at its current size.
// Split out from showWelcome because layout calls it on every pass while the
// panel is empty: the block is centered on the panel's size, which only layout
// knows has changed.
func (gui *Gui) renderWelcome(view *gocui.View) {
	width, height := view.InnerSize()

	view.SetContent(welcomeContent(gui.welcomeSubtitle(), gui.welcomeHints(), width, height))
}
