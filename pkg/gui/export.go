package gui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// exportSession prompts for a path and dumps the selected session's whole
// scrollback (plus its still-live screen) into it — the "w" binding. No-op,
// same as killSession/renameSession, if there is no session to export.
func (gui *Gui) exportSession(*gocui.Gui, *gocui.View) error {
	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	return gui.showPrompt(gui.tr.T("prompt.export"), defaultExportPath(sess), func(path string) error {
		return gui.onExportSubmit(sess, path)
	})
}

// defaultExportPath is exportSession's pre-filled suggestion: the session's
// own cwd (always set — it is the directory the session was created in,
// never empty), named after the session and timestamped so repeated exports
// of the same session never collide by default. A pure function so it is
// testable without a Gui.
func defaultExportPath(sess *session.Session) string {
	timestamp := time.Now().Format("20060102-150405")

	return filepath.Join(sess.Cwd, sess.Name()+"-"+timestamp+".log")
}

// onExportSubmit is exportSession's onSubmit: an empty path cancels, same
// convention as renameSession. Deliberately no O_EXCL, unlike
// pkg/app/init.go's project-file template: those guard a hand-edited file
// from being clobbered, but an export is a disposable capture the user asks
// for again on purpose (re-running "w" at the same path to refresh a bug
// report) — overwriting is the point, not a mistake to prevent.
//
// Both outcomes are reported from here rather than through showPrompt's
// contract (a returned error becoming lastError in submitPrompt's tail): the
// write runs behind busy.go's spinner, so it has already outlived submitPrompt
// by the time it fails, and there is nobody left up the stack to hand an error
// to. runBusyThen's own tail is that "up the stack" instead — one branch for
// the failure, one for the success reportSessionInfo has always carried.
func (gui *Gui) onExportSubmit(sess *session.Session, path string) error {
	if path == "" {
		return nil
	}

	// Read here, on gocui's goroutine, not inside the op below: the emulator
	// is shared with the render task, and TextRange is cheap next to the write
	// it feeds — a whole scrollback to a slow or networked filesystem is the
	// part worth putting behind a spinner.
	text := sess.Screen().TextRange(0, math.MaxInt)

	return gui.runBusyThen(gui.tr.T("busy.export", path), func() error {
		return os.WriteFile(path, []byte(text), 0o644)
	}, func(err error) error {
		if err != nil {
			return gui.reportSessionError(fmt.Errorf("%s", gui.tr.T("export.failed", err)))
		}

		return gui.reportSessionInfo(gui.tr.T("export.success", path))
	})
}
