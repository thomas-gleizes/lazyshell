package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"

	"github.com/thomas-gleizes/lazyshell/pkg/session"
)

// exportTo drives a full "w" export from the sessions panel: opens the
// prompt (as exportSession would), replaces its pre-filled default path with
// path, and submits — the same sequence search_test.go's searchFor and
// filter_test.go's filterFor use for their own prompts.
func exportTo(t *testing.T, gui *Gui, g *gocui.Gui, path string) {
	t.Helper()

	if err := gui.exportSession(g, nil); err != nil {
		t.Fatalf("exportSession failed: %v", err)
	}

	view, err := g.View(promptViewName)
	if err != nil {
		t.Fatalf("prompt view not found: %v", err)
	}
	view.Clear()
	view.SetOrigin(0, 0)
	if _, err := view.Write([]byte(path)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := gui.submitPrompt(g, view); err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}
}

// newExportTestGui builds a Gui with one session fed deterministic content,
// its views laid out (export's prompt needs sessionsViewName/statusViewName
// to already exist for closePrompt/renderStatus, the same requirement
// filter_test.go's prompt-driving tests ran into).
func newExportTestGui(t *testing.T) (*Gui, *gocui.Gui, *session.Session) {
	t.Helper()

	gui, g := newHeadlessGui(t)
	sess := newTestSession(t, gui, "t")
	feed(t, sess, "exported-content-marker\r\n")

	for range 2 {
		if err := gui.layout(g); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}

	return gui, g, sess
}

func TestDefaultExportPathUsesSessionNameAndCwd(t *testing.T) {
	m := session.NewManager()
	t.Cleanup(m.Shutdown)

	sess, err := m.NewInDir("my-session", "/bin/sh", t.TempDir())
	if err != nil {
		t.Fatalf("NewInDir: %v", err)
	}

	path := defaultExportPath(sess)

	if !strings.HasPrefix(path, sess.Cwd) {
		t.Errorf("defaultExportPath() = %q, want it rooted at %q", path, sess.Cwd)
	}
	if !strings.Contains(filepath.Base(path), sess.Name()) {
		t.Errorf("defaultExportPath() = %q, want it to contain the session name %q", path, sess.Name())
	}
	if !strings.HasSuffix(path, ".log") {
		t.Errorf("defaultExportPath() = %q, want a .log suffix", path)
	}
}

func TestExportSessionWritesFullScrollbackToFile(t *testing.T) {
	gui, g, _ := newExportTestGui(t)

	tmpFile := filepath.Join(t.TempDir(), "out.log")
	exportTo(t, gui, g, tmpFile)

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("reading exported file: %v", err)
	}

	if !strings.Contains(string(data), "exported-content-marker") {
		t.Errorf("exported content = %q, want it to contain the fed marker", data)
	}
}

func TestExportSessionReportsSuccessAndClearsError(t *testing.T) {
	gui, g, _ := newExportTestGui(t)
	gui.lastError = "a stale error from something else"

	tmpFile := filepath.Join(t.TempDir(), "out.log")
	exportTo(t, gui, g, tmpFile)

	if gui.lastError != "" {
		t.Errorf("lastError = %q, want it cleared by a successful export", gui.lastError)
	}
	if !strings.Contains(gui.lastInfo, tmpFile) {
		t.Errorf("lastInfo = %q, want it to mention the export path %q", gui.lastInfo, tmpFile)
	}
}

func TestExportSessionEmptyPathCancelsSilently(t *testing.T) {
	gui, g, _ := newExportTestGui(t)

	exportTo(t, gui, g, "")

	if gui.lastError != "" {
		t.Errorf("lastError = %q, want an empty path to cancel without error", gui.lastError)
	}
	if gui.lastInfo != "" {
		t.Errorf("lastInfo = %q, want an empty path to leave no success message", gui.lastInfo)
	}
}

func TestExportSessionReportsWriteFailure(t *testing.T) {
	gui, g, _ := newExportTestGui(t)

	exportTo(t, gui, g, filepath.Join(t.TempDir(), "no-such-dir", "out.log"))

	if gui.lastError == "" {
		t.Error("expected a status-bar error for a write to a nonexistent directory")
	}
	if gui.lastInfo != "" {
		t.Errorf("lastInfo = %q, want it left empty by a failed export", gui.lastInfo)
	}
}

func TestExportIsNoopWithoutASelectedSession(t *testing.T) {
	gui, g := newHeadlessGui(t)

	if err := gui.exportSession(g, nil); err != nil {
		t.Fatalf("exportSession: %v", err)
	}

	if _, err := g.View(promptViewName); err == nil {
		t.Error("exportSession opened a prompt despite no session being selected")
	}
}

func TestSessionsFooterShowsExportHint(t *testing.T) {
	gui, _ := newHeadlessGui(t)

	got := gui.footerText(gui.sessionsFooterHints(), 200)
	if !strings.Contains(got, "w:"+gui.tr.T("footer.export")) {
		t.Errorf("sessions footer = %q, want a 'w' hint for exporting", got)
	}
}

func TestReportSessionInfoClearsLastError(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.lastError = "boom"

	_ = gui.reportSessionInfo("done")

	if gui.lastError != "" {
		t.Errorf("lastError = %q, want reportSessionInfo to clear it", gui.lastError)
	}
	if gui.lastInfo != "done" {
		t.Errorf("lastInfo = %q, want %q", gui.lastInfo, "done")
	}
}

func TestReportSessionErrorClearsLastInfo(t *testing.T) {
	gui, _ := newHeadlessGui(t)
	gui.lastInfo = "a stale success message"

	_ = gui.reportSessionError(fmt.Errorf("boom"))

	if gui.lastInfo != "" {
		t.Errorf("lastInfo = %q, want reportSessionError to clear it", gui.lastInfo)
	}
	if gui.lastError != "boom" {
		t.Errorf("lastError = %q, want %q", gui.lastError, "boom")
	}
}
