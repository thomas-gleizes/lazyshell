package gui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// copyToClipboard is copy-mode's yank: OSC 52, or the configured fallback
// command instead of it. There is no reliable way for lazyshell to learn
// whether the host terminal actually accepted an OSC 52 sequence — it is a
// write with no acknowledgement — so pkg/config's Clipboard.FallbackCommand
// is a manual switch rather than something detected automatically: unset,
// OSC 52 is used (works through SSH, needs no binary installed); set, the
// command runs instead, with text on its stdin, for a terminal that does not
// support OSC 52.
func (gui *Gui) copyToClipboard(text string) error {
	if gui.clipboardFallback != "" {
		return runClipboardFallback(gui.clipboardFallback, text)
	}

	return oscClipboardWrite(text)
}

// oscClipboardWrite writes text to the host terminal's clipboard via OSC 52.
// Written straight to the real terminal file descriptor gocui is driving —
// os.Stdout, which gocui never redirects, only draws over — not into a
// gocui.View: a view's content lives inside lazyshell's own rendered screen,
// invisible to the outer terminal that owns the actual clipboard.
func oscClipboardWrite(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))

	_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)

	return err
}

// runClipboardFallback runs command through the shell with text on its
// stdin — the same shape "sh -c" gives any other user-supplied command in
// this codebase.
func runClipboardFallback(command, text string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = strings.NewReader(text)

	return cmd.Run()
}
