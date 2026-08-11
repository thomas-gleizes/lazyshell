package session

import (
	"bytes"
	"regexp"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// watchCooldown bounds how often a single armed pattern (config-declared or
// runtime-armed) may produce a fresh WatchHit — the anti-rebond ROADMAP.md's
// entry for this feature asked to be resolved before code: a build log
// spraying 200 matching lines in a burst must fire one notification, not 200.
// A match inside the window is dropped outright, not queued or coalesced —
// unlike evaluateAgentState's trailing-edge timer, a watch has no "final
// state" it must eventually settle on, so the next real match once the
// window reopens is enough.
const watchCooldown = 3 * time.Second

// watchLineBufMax caps how many unterminated bytes feedWatch accumulates
// before giving up on a newline ever arriving and matching what it has. A
// program that never emits '\n' (a spinner driven entirely by '\r', a binary
// dump) must not turn this into unbounded growth — the same defensive-cap
// instinct as agentScreenTailLines above.
const watchLineBufMax = 4096

// WatchSpec is one pattern watcher, as a caller of Options.Watch declares it.
// Deliberately the same two fields as pkg/config's own WatchSpec — kept as a
// separate type rather than importing pkg/config, the same "plain values
// only, no shared struct" boundary Options already draws for every other
// field ResolvedSession also carries.
type WatchSpec struct {
	Pattern string
	Notify  bool
}

// WatchHit is the latest notify-eligible pattern match, polled by pkg/gui
// the same way Screen.LastCommandExit is: Seq is what a caller compares
// against its own last-seen value to detect a fresh hit without missing one
// or firing twice for the same one.
type WatchHit struct {
	Pattern string
	Line    string
	Notify  bool
	Seq     uint64
}

// watch is one compiled, armed pattern.
type watch struct {
	pattern   string
	notify    bool
	re        *regexp.Regexp
	lastFired time.Time
}

// setConfigWatchers compiles specs into this session's fixed set of
// config-declared watchers. Called once, from newSession, before drain
// starts — no lock needed yet, nothing else can see this session. A pattern
// that fails to compile is dropped silently: config.Validate already caught
// this for anything that went through a project file, so reaching here means
// a caller built Options by hand (tests) and gets the same "gutter hint,
// never a hard dependency" treatment agent detection gets.
func (s *Session) setConfigWatchers(specs []WatchSpec) {
	for _, spec := range specs {
		re, err := regexp.Compile(spec.Pattern)
		if err != nil {
			continue
		}

		s.watchers = append(s.watchers, &watch{pattern: spec.Pattern, notify: spec.Notify, re: re})
	}
}

// ArmWatch sets (or, given "", clears) the session's single runtime-armed
// pattern — the 'v' keybinding's onSubmit, passed to showPrompt directly
// since the signatures already match. Unlike renameSession's empty-input
// no-op (a session's name must never be empty), an empty pattern here clears
// the runtime watch rather than doing nothing: "no runtime watch armed" is a
// normal, useful state, and blanking the prompt is the discoverable way to
// reach it. Config-declared watchers (setConfigWatchers) are untouched
// either way.
func (s *Session) ArmWatch(pattern string) error {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	if pattern == "" {
		s.runtimeWatch = nil

		return nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	s.runtimeWatch = &watch{pattern: pattern, notify: true, re: re}

	return nil
}

// RuntimeWatchPattern is the session's current runtime-armed pattern, "" for
// none — used to prefill the 'v' prompt so re-arming shows what is already
// active instead of a blank box.
func (s *Session) RuntimeWatchPattern() string {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	if s.runtimeWatch == nil {
		return ""
	}

	return s.runtimeWatch.pattern
}

// LastWatchHit is the most recent notify-eligible pattern match, polled by
// pkg/gui/notify.go's checkWatchNotifications on its own tick — the same
// poll-and-edge-detect shape as AgentState/LastCommandExit, not a push. ok is
// false until the first hit this session has ever produced.
func (s *Session) LastWatchHit() (WatchHit, bool) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	return s.lastWatchHit, s.watchSeq > 0
}

// feedWatch is sessionTapWriter's other per-chunk side effect: it buffers p
// into complete lines and matches each one against every armed watcher.
//
// Matching is against ansi.Strip'd plain text, not raw bytes — a colored
// "ERR!" is still "ERR!" once its escape codes are gone, and a pattern
// written against what the panel visibly shows should not have to account
// for how it got there.
//
// Skipped entirely while the screen is in alt-screen mode, and the buffer is
// reset on every alt-screen transition in either direction: a vim or htop
// redraw can write anything, including bytes that would look like a
// newline-delimited log line by accident, and a partial line buffered right
// before the transition must never be stitched to bytes from the other side
// of it. Same reasoning ADR 0008 already applied to OSC 133 boundaries.
func (s *Session) feedWatch(p []byte) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	if len(s.watchers) == 0 && s.runtimeWatch == nil {
		return
	}

	altScreen := s.screen.IsAltScreen()
	if altScreen != s.wasAltScreen {
		s.lineBuf = nil
		s.wasAltScreen = altScreen
	}

	if altScreen {
		return
	}

	s.lineBuf = append(s.lineBuf, p...)

	for {
		i := bytes.IndexByte(s.lineBuf, '\n')
		if i < 0 {
			break
		}

		line := s.lineBuf[:i]
		s.lineBuf = s.lineBuf[i+1:]
		s.checkWatchLineLocked(string(line))
	}

	if len(s.lineBuf) > watchLineBufMax {
		s.checkWatchLineLocked(string(s.lineBuf))
		s.lineBuf = nil
	}
}

// checkWatchLineLocked matches line against every armed watcher. Callers
// must hold watchMu.
func (s *Session) checkWatchLineLocked(line string) {
	plain := ansi.Strip(line)

	for _, w := range s.watchers {
		s.matchWatchLocked(w, plain)
	}

	if s.runtimeWatch != nil {
		s.matchWatchLocked(s.runtimeWatch, plain)
	}
}

// matchWatchLocked applies w to plain, subject to watchCooldown. Callers
// must hold watchMu.
func (s *Session) matchWatchLocked(w *watch, plain string) {
	if !w.re.MatchString(plain) {
		return
	}

	now := time.Now()
	if now.Sub(w.lastFired) < watchCooldown {
		return
	}
	w.lastFired = now

	s.watchSeq++
	s.lastWatchHit = WatchHit{Pattern: w.pattern, Line: plain, Notify: w.notify, Seq: s.watchSeq}
}
