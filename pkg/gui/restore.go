package gui

import (
	"strings"

	"github.com/thomas-gleizes/lazyshell/pkg/config"
	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// SetPendingRestore records a saved layout (config.LoadState) for Run to
// offer once the terminal is up, when Config.RestoreLayout is "ask". Must be
// called before Run, like SetStartupError — pendingRestore is read with no
// lock, which is only sound because nothing writes it once the interface is
// up.
func (gui *Gui) SetPendingRestore(state *config.StateFile, shell string) {
	gui.pendingRestore = state
	gui.restoreShell = shell
}

// PendingRestore reports what SetPendingRestore recorded, for pkg/app's
// bootstrap tests — same purpose as GroupOrder/LockedSessions/StartupError.
func (gui *Gui) PendingRestore() (*config.StateFile, string) {
	return gui.pendingRestore, gui.restoreShell
}

// confirmRestoreLayout is the popup Run queues when a pending restore was
// recorded. Declining leaves the session list exactly as empty as it was —
// no fallback session is created — the same state `--no-autostart` leaves
// the user in, and "n" is right there.
func (gui *Gui) confirmRestoreLayout() error {
	state := gui.pendingRestore
	gui.pendingRestore = nil

	names := make([]string, 0, len(state.Sessions))
	for _, spec := range state.Sessions {
		names = append(names, spec.Name)
	}

	message := gui.tr.T("restore.confirm", len(state.Sessions), strings.Join(names, ", "))

	return gui.showConfirm(message, func() error {
		return gui.applyRestoredLayout(state)
	})
}

// applyRestoredLayout is confirmRestoreLayout's onConfirm, factored out so it
// can be exercised directly in tests without going through gocui's key
// dispatch — same reasoning as acceptConfirm (pkg/gui/confirm.go).
func (gui *Gui) applyRestoredLayout(state *config.StateFile) error {
	shell := gui.restoreShell
	if shell == "" {
		shell = gui.defaultShell()
	}

	for _, spec := range state.Sessions {
		if _, err := gui.createSessionWithOptions(session.Options{
			Name:    spec.Name,
			Group:   spec.Group,
			Shell:   shell,
			Cwd:     spec.Cwd,
			Command: spec.Command,
		}); err != nil {
			gui.appendStartupError(err.Error())
		}
	}

	gui.onSelectionChanged()

	return nil
}
