//go:build linux

package session

import (
	"fmt"
	"os"
	"strings"
)

// foregroundProcessName reports the name of the process group leader
// currently in the foreground of ptmx — the "quel agent tourne" signal
// pkg/agent needs to pick a manifest. It says *who*, never *what*: the
// answer is a bare process name (e.g. "claude"), never a state.
func foregroundProcessName(ptmx *os.File) (string, error) {
	pgid, err := foregroundPGID(ptmx)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pgid))
	if err != nil {
		return "", fmt.Errorf("session: read comm for pgid %d: %w", pgid, err)
	}

	return strings.TrimSpace(string(data)), nil
}
