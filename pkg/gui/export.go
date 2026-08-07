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
// A write failure is returned as-is, following showPrompt's own contract
// (see newSessionInDir): submitPrompt's generic tail turns a returned error
// into the status bar's lastError, so nothing here needs to duplicate that.
// Success is different — there is no existing "it worked" channel — so this
// reports it itself via reportSessionInfo, which submitPrompt's tail leaves
// alone (it only ever touches lastError).
func (gui *Gui) onExportSubmit(sess *session.Session, path string) error {
	if path == "" {
		return nil
	}

	text := sess.Screen().TextRange(0, math.MaxInt)

	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("%s", gui.tr.T("export.failed", err))
	}

	return gui.reportSessionInfo(gui.tr.T("export.success", path))
}
