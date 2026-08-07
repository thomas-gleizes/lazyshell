package gui

import (
	"os/exec"
	"strings"
	"time"
)

// statsRefreshInterval bounds how often agentStatsCommand is actually run:
// a token/cost command is typically not cheap and does not need sub-second
// freshness, unlike TurnDuration's plain time.Since. Named independently of
// pkg/session's agentCheckInterval — same shape, different package, a
// user-supplied external command rather than a syscall.
const statsRefreshInterval = 5 * time.Second

// statsLinesForRender returns the map sessionsPanelContent expects: at most
// one entry, for whichever session AgentStatsCommand was last computed for
// — there is no case where more than one session's line is ever cached (see
// refreshAgentStats), but the map shape keeps sessionsPanelContent's
// signature symmetric with markers/broadcastMarks rather than special-cased
// for "just the selected session".
func (gui *Gui) statsLinesForRender() map[string]string {
	gui.mu.Lock()
	defer gui.mu.Unlock()

	if gui.statsLine == "" {
		return nil
	}

	return map[string]string{gui.statsSessionID: gui.statsLine}
}

// refreshAgentStats runs pkg/config's AgentStatsCommand for the selected
// session and caches its first line of output — throttled to
// statsRefreshInterval, and only for the selected session: spawning a
// process per tick for every open agent session would be a real cost, unlike
// the plain time.Since TurnDuration already shows for all of them.
//
// A no-op when AgentStatsCommand is unset (the default) or no session is
// selected. Runs the command in its own goroutine so a slow one cannot
// stall the shared periodic tick — same reasoning as notify.go's fallback
// command.
func (gui *Gui) refreshAgentStats() error {
	if gui.agentStatsCommand == "" {
		return nil
	}

	sess := gui.selectedSession()
	if sess == nil {
		return nil
	}

	gui.mu.Lock()
	fresh := sess.ID == gui.statsSessionID && time.Since(gui.statsCheckedAt) < statsRefreshInterval
	shouldRun := !fresh && !gui.statsPending
	if shouldRun {
		gui.statsPending = true
		gui.statsCheckedAt = time.Now()
	}
	gui.mu.Unlock()

	if !shouldRun {
		return nil
	}

	go gui.runAgentStatsCommand(sess.ID)

	return nil
}

// runAgentStatsCommand actually spawns AgentStatsCommand and caches its
// output — split from refreshAgentStats so the throttle check (cheap, must
// run on every tick) and the process spawn (not cheap, must not block the
// tick) are two different pieces of work.
func (gui *Gui) runAgentStatsCommand(sessionID string) {
	cmd := exec.Command("sh", "-c", gui.agentStatsCommand)
	cmd.Env = append(cmd.Environ(), "LAZYSHELL_SESSION_ID="+sessionID)

	output, err := cmd.Output()

	line := ""
	if err == nil {
		if first, _, _ := strings.Cut(string(output), "\n"); first != "" {
			line = strings.TrimSpace(first)
		}
	}

	gui.mu.Lock()
	gui.statsSessionID = sessionID
	gui.statsLine = line
	gui.statsPending = false
	gui.mu.Unlock()
}
