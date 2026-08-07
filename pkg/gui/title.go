package gui

import (
	"fmt"
	"os"
)

// updateWindowTitle pushes the selected session's name — plus whatever OSC
// 0/2 title the program running inside its pty last set (pkg/screen's
// Screen.Title, e.g. what vim or ssh set) when it has set one — to the host
// terminal's own window/tab title via OSC 0. Called both on selection change
// (onSelectionChanged, for an immediate update) and on the shared tick in
// Run (for a live title that changes without the selection changing, e.g.
// vim switching buffers).
func (gui *Gui) updateWindowTitle() {
	if !gui.windowTitleEnabled {
		return
	}

	sess := gui.selectedSession()
	if sess == nil {
		return
	}

	title := sess.Name()
	if live := sess.Screen().Title(); live != "" {
		title = fmt.Sprintf("%s: %s", title, live)
	}
	title = "lazyshell — " + title

	gui.mu.Lock()
	unchanged := title == gui.lastWindowTitle
	gui.lastWindowTitle = title
	gui.mu.Unlock()

	if unchanged {
		return
	}

	oscTitleWrite(title)
}

// resetWindowTitle clears the host terminal's window/tab title, deferred
// from Run so lazyshell does not leave the last selected session's title
// behind after it quits.
func (gui *Gui) resetWindowTitle() {
	if !gui.windowTitleEnabled {
		return
	}

	oscTitleWrite("")
}

// oscTitleWrite writes title to the host terminal via OSC 0 (icon + window
// title) — same os.Stdout target as oscNotifyWrite/oscClipboardWrite: the
// real terminal gocui only ever draws over, never redirects.
func oscTitleWrite(title string) {
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]0;%s\x07", title)
}
