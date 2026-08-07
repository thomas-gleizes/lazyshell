//go:build darwin

package session

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Darwin has no /proc, and the two numbers that matter — cumulative CPU time
// and resident memory — are not in the kinfo_proc that unix.SysctlKinfoProc
// exposes (its Eproc.Xrssize is a legacy int16 the kernel no longer fills in
// meaningfully). They live behind libproc's proc_pidinfo(PROC_PIDTASKINFO),
// which means cgo, which this project does not use.
//
// So the sample comes from `ps`, in a single spawn for every pid at once. That
// is a real cost, but a bounded one: the perf tab throttles sampling to about
// once a second, the same order of magnitude pkg/gui's AgentStatsCommand
// already spends per session, and nothing else in lazyshell ever calls this.
const psPath = "/bin/ps"

// psTimeout bounds the spawn: `ps` on a healthy system answers in
// milliseconds, and a sampler that can hang has no business running on a
// render task's goroutine.
const psTimeout = 2 * time.Second

// sampleProcs runs one `ps` for every requested pid. The output columns are
// ordered so that comm — the only one that can contain spaces, since macOS
// reports a full executable path — comes last and can absorb the remainder of
// the line. `ps` also reorders its rows by pid, hence the map keyed on the pid
// it actually reported rather than on request order.
func sampleProcs(pids []int) map[int]ProcStats {
	out := make(map[int]ProcStats, len(pids))
	if len(pids) == 0 {
		return out
	}

	list := make([]string, 0, len(pids))
	for _, pid := range pids {
		list = append(list, strconv.Itoa(pid))
	}

	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, psPath, "-o", "pid=,rss=,time=,comm=", "-p", strings.Join(list, ","))

	// ps exits non-zero when *none* of the pids exist; a partial answer still
	// comes back on stdout, so the output is parsed either way and an empty
	// result is what tells the caller nothing was readable.
	raw, _ := cmd.Output()

	now := time.Now()

	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		stats, ok := parsePSLine(line)
		if !ok {
			continue
		}

		stats.SampledAt = now
		out[stats.PID] = stats
	}

	return out
}

// parsePSLine turns one "pid rss time comm" row into a sample. Everything
// darwin cannot answer without cgo is left at its zero value with the matching
// Available flag false — the panel says "unknown", never "zero".
func parsePSLine(line string) (ProcStats, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ProcStats{}, false
	}

	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return ProcStats{}, false
	}

	// ps reports rss in kibibytes.
	rssKiB, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return ProcStats{}, false
	}

	cpu, ok := parsePSTime(fields[2])
	if !ok {
		return ProcStats{}, false
	}

	return ProcStats{
		PID: pid,
		// A full path here would eat the panel's width and say nothing the
		// name does not; the linux side reports a bare comm, and the two must
		// agree on what Comm means.
		Comm:     filepath.Base(strings.Join(fields[3:], " ")),
		CPUTime:  cpu,
		RSSBytes: rssKiB * 1024,
		// No thread count and no per-process disk I/O without libproc — see
		// the file comment.
		ThreadsAvailable: false,
		DiskIOAvailable:  false,
	}, true
}

// parsePSTime reads ps's cumulative-time column. macOS prints "MM:SS.ss" and
// lets the minutes run past 60 ("133:41.49"), but ps(1) documents the wider
// "[dd-]hh:mm:ss" form, so both are accepted: the components are read from the
// right, seconds first.
func parsePSTime(s string) (time.Duration, bool) {
	days := 0

	if before, after, found := strings.Cut(s, "-"); found {
		parsed, err := strconv.Atoi(before)
		if err != nil {
			return 0, false
		}

		days, s = parsed, after
	}

	parts := strings.Split(s, ":")
	if len(parts) == 0 || len(parts) > 3 {
		return 0, false
	}

	total := time.Duration(days) * 24 * time.Hour
	unit := time.Second

	for i := len(parts) - 1; i >= 0; i-- {
		value, err := strconv.ParseFloat(parts[i], 64)
		if err != nil {
			return 0, false
		}

		total += time.Duration(value * float64(unit))
		unit *= 60
	}

	return total, true
}
