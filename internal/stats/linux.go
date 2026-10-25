package stats

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Linux reads the statistics from the files Linux keeps in /proc and /sys,
// under Root, which is "/" but for tests.
type Linux struct {
	Root string
}

// CPU reads /proc/stat.
func (l Linux) CPU() (CPU, error) {
	f, err := os.Open(filepath.Join(l.Root, "proc", "stat"))
	if err != nil {
		return CPU{}, err
	}
	defer f.Close()
	return parseStat(f)
}

// Memory reads /proc/meminfo.
func (l Linux) Memory() (Memory, error) {
	f, err := os.Open(filepath.Join(l.Root, "proc", "meminfo"))
	if err != nil {
		return Memory{}, err
	}
	defer f.Close()
	return parseMeminfo(f)
}

// Network reads the counters of each adapter in /sys/class/net. An adapter
// whose counters cannot be read, as one that went away while they were being
// read, is left out.
func (l Linux) Network() (map[string]NetCounters, error) {
	dir := filepath.Join(l.Root, "sys", "class", "net")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	counters := make(map[string]NetCounters, len(entries))
	for _, e := range entries {
		statistics := filepath.Join(dir, e.Name(), "statistics")
		received, err := readCounter(filepath.Join(statistics, "rx_bytes"))
		if err != nil {
			continue
		}
		sent, err := readCounter(filepath.Join(statistics, "tx_bytes"))
		if err != nil {
			continue
		}
		counters[e.Name()] = NetCounters{Received: received, Sent: sent}
	}
	return counters, nil
}

// readCounter reads a file that holds a single number.
func readCounter(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(bytes.TrimSpace(data)), 10, 64)
}

// parseStat reads the lines of /proc/stat about the processors: "cpu", which
// is all of them, and "cpu0", "cpu1" and so on. Their columns are user, nice,
// system, idle, iowait, irq, softirq, steal, guest and guest_nice; older
// kernels have fewer. Guest time is counted in user time already, so it is
// left out.
func parseStat(r io.Reader) (CPU, error) {
	var c CPU
	found := false
	s := bufio.NewScanner(r)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		var ticks [8]uint64
		for i := 1; i < len(fields) && i <= len(ticks); i++ {
			n, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return CPU{}, fmt.Errorf("/proc/stat: %s: %w", fields[0], err)
			}
			ticks[i-1] = n
		}
		const iowait, idle = 4, 3
		var t CPUTimes
		for i, n := range ticks {
			if i == idle || i == iowait {
				t.Idle += n
			} else {
				t.Busy += n
			}
		}
		if fields[0] == "cpu" {
			c.All, found = t, true
			continue
		}
		// A processor that is offline has no line, so a gap in the numbers
		// is left as one that has spent no time.
		n, err := strconv.Atoi(fields[0][len("cpu"):])
		if err != nil || n < 0 {
			continue
		}
		for len(c.Cores) <= n {
			c.Cores = append(c.Cores, CPUTimes{})
		}
		c.Cores[n] = t
	}
	if err := s.Err(); err != nil {
		return CPU{}, err
	}
	if !found {
		return CPU{}, errors.New("/proc/stat: no line for the processors")
	}
	return c, nil
}

// parseMeminfo reads /proc/meminfo, whose values are in kibibytes. Memory in
// use is what is not available to be given to programs, which leaves out
// caches that can be dropped. Kernels older than 3.14 do not say what is
// available, and it is reckoned from what is free and cached there.
func parseMeminfo(r io.Reader) (Memory, error) {
	values := map[string]uint64{}
	s := bufio.NewScanner(r)
	for s.Scan() {
		name, rest, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[name] = n * 1024
	}
	if err := s.Err(); err != nil {
		return Memory{}, err
	}
	total, ok := values["MemTotal"]
	if !ok {
		return Memory{}, errors.New("/proc/meminfo: no MemTotal")
	}
	available, ok := values["MemAvailable"]
	if !ok {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	m := Memory{
		Total:     total,
		Used:      total - min(available, total),
		SwapTotal: values["SwapTotal"],
	}
	m.SwapUsed = m.SwapTotal - min(values["SwapFree"], m.SwapTotal)
	return m, nil
}
