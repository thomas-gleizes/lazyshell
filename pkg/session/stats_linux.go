//go:build linux

package session

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// clockTicksPerSecond is what /proc/<pid>/stat's utime/stime are counted in.
// Hardcoded rather than read from sysconf(_SC_CLK_TCK), which needs cgo: the
// kernel's USER_HZ has been 100 on every architecture Go targets for Linux,
// and it is an ABI constant that cannot change without breaking every
// /proc reader in existence.
const clockTicksPerSecond = 100

// sampleProcs reads each pid's usage from /proc. Processes that cannot be read
// (already gone, or not ours) are simply absent from the result — the caller
// treats a missing entry as "not there any more", which is the common case for
// a short-lived foreground command.
func sampleProcs(pids []int) map[int]ProcStats {
	out := make(map[int]ProcStats, len(pids))

	for _, pid := range pids {
		stats, err := readProcStat(pid)
		if err != nil {
			continue
		}

		stats.DiskRead, stats.DiskWritten, stats.DiskIOAvailable = readProcIO(pid)
		out[pid] = stats
	}

	return out
}

// readProcStat parses /proc/<pid>/stat. Field numbers below are the ones
// proc(5) documents, 1-based, and the offsets they map to here are shifted by
// the comm-parsing rule: the name is in parentheses and may itself contain
// spaces and parentheses ("(tmux: server)"), so everything after the *last*
// ')' is re-split, making the first field of that remainder field 3 (state).
func readProcStat(pid int) (ProcStats, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ProcStats{}, fmt.Errorf("session: read stat for pid %d: %w", pid, err)
	}

	line := string(raw)

	openIdx := strings.IndexByte(line, '(')
	closeIdx := strings.LastIndexByte(line, ')')
	if openIdx < 0 || closeIdx < openIdx {
		return ProcStats{}, fmt.Errorf("session: malformed stat for pid %d", pid)
	}

	comm := line[openIdx+1 : closeIdx]
	fields := strings.Fields(line[closeIdx+1:])

	// fields[i] is proc(5)'s field i+3 — see the comment above.
	const (
		utimeIdx      = 11 // field 14
		stimeIdx      = 12 // field 15
		numThreadsIdx = 17 // field 20
		rssIdx        = 21 // field 24
	)

	if len(fields) <= rssIdx {
		return ProcStats{}, fmt.Errorf("session: short stat for pid %d", pid)
	}

	utime, err := strconv.ParseUint(fields[utimeIdx], 10, 64)
	if err != nil {
		return ProcStats{}, fmt.Errorf("session: utime for pid %d: %w", pid, err)
	}

	stime, err := strconv.ParseUint(fields[stimeIdx], 10, 64)
	if err != nil {
		return ProcStats{}, fmt.Errorf("session: stime for pid %d: %w", pid, err)
	}

	threads, err := strconv.Atoi(fields[numThreadsIdx])
	if err != nil {
		return ProcStats{}, fmt.Errorf("session: num_threads for pid %d: %w", pid, err)
	}

	// rss is in pages, not bytes — the one field in this file that is not
	// already in a unit anyone would expect.
	rssPages, err := strconv.ParseUint(fields[rssIdx], 10, 64)
	if err != nil {
		return ProcStats{}, fmt.Errorf("session: rss for pid %d: %w", pid, err)
	}

	ticks := utime + stime

	return ProcStats{
		PID:              pid,
		Comm:             comm,
		CPUTime:          time.Duration(ticks) * time.Second / clockTicksPerSecond,
		RSSBytes:         rssPages * uint64(os.Getpagesize()),
		Threads:          threads,
		ThreadsAvailable: true,
		SampledAt:        time.Now(),
	}, nil
}

// readProcIO reads the two counters of /proc/<pid>/io that mean "actually
// touched storage" — read_bytes/write_bytes rather than rchar/wchar, which
// count every read() including the ones served from page cache.
//
// The file is unreadable on a hardened kernel (EACCES even for one's own
// processes, under some LSM policies) and absent when the kernel was built
// without CONFIG_TASK_IO_ACCOUNTING. Both answer false rather than zero: the
// panel must be able to say "unknown" instead of claiming no I/O happened.
func readProcIO(pid int) (read, written uint64, ok bool) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/io", pid))
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()

	var seen int

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, found := strings.Cut(scanner.Text(), ": ")
		if !found {
			continue
		}

		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}

		switch name {
		case "read_bytes":
			read, seen = parsed, seen+1
		case "write_bytes":
			written, seen = parsed, seen+1
		}
	}

	return read, written, seen == 2
}
