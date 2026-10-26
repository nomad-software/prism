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
	"time"
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

// userHZ is how many ticks a second the times of processes in /proc are
// counted in: USER_HZ, which Linux keeps at 100 for every program, whatever
// its own clock runs at.
const userHZ = 100

// Processes reads /proc/<pid>/stat, /proc/<pid>/statm and /proc/<pid>/status
// of every process. A process that cannot be read, as one that ended while the
// others were being read, is left out.
func (l Linux) Processes() ([]Process, error) {
	dir := filepath.Join(l.Root, "proc")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	page := uint64(os.Getpagesize())
	var processes []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "stat"))
		if err != nil {
			continue
		}
		p, err := parseProcessStat(data)
		if err != nil {
			continue
		}
		data, err = os.ReadFile(filepath.Join(dir, e.Name(), "statm"))
		if err != nil {
			continue
		}
		resident, err := parseStatm(data)
		if err != nil {
			continue
		}
		data, err = os.ReadFile(filepath.Join(dir, e.Name(), "status"))
		if err != nil {
			continue
		}
		uid, err := parseStatusUID(data)
		if err != nil {
			continue
		}
		p.PID, p.UID, p.Memory = pid, uid, resident*page
		processes = append(processes, p)
	}
	return processes, nil
}

// Signal sends the signal to the process, if /proc/<pid>/stat says it
// started when it did.
func (l Linux) Signal(p Process, number int) error {
	return signal(p, func(pid int) (uint64, bool) {
		data, err := os.ReadFile(filepath.Join(l.Root, "proc", strconv.Itoa(pid), "stat"))
		if err != nil {
			return 0, false
		}
		now, err := parseProcessStat(data)
		return now.Start, err == nil
	}, number)
}

// pfKthread is the flag of a process in /proc/<pid>/stat that says it is a
// thread of the kernel, PF_KTHREAD.
const pfKthread = 0x00200000

// parseProcessStat reads the name, times and start of a process from its
// /proc/<pid>/stat: "1234 (name) S 1 ...", the PID of its parent, and
// whether it is a thread of the kernel. The name is in brackets, and can
// have spaces and brackets of its own, so the fields are those after the
// last closing bracket. Of them, ppid is the 2nd, flags the 7th, utime and
// stime the 12th and 13th, and starttime the 20th, in ticks of userHZ.
func parseProcessStat(data []byte) (Process, error) {
	s := string(bytes.TrimSpace(data))
	left, right := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if left < 0 || right < left {
		return Process{}, errors.New("stat: no name")
	}
	fields := strings.Fields(s[right+1:])
	const ppid, flags, utime, stime, starttime = 1, 6, 11, 12, 19
	if len(fields) <= starttime {
		return Process{}, errors.New("stat: too few fields")
	}
	var n [5]uint64
	for i, f := range []int{utime, stime, starttime, flags, ppid} {
		v, err := strconv.ParseUint(fields[f], 10, 64)
		if err != nil {
			return Process{}, fmt.Errorf("stat: %w", err)
		}
		n[i] = v
	}
	return Process{
		Name:   s[left+1 : right],
		CPU:    time.Duration(n[0]+n[1]) * (time.Second / userHZ),
		Start:  n[2],
		Kernel: n[3]&pfKthread != 0,
		PPID:   int(n[4]),
	}, nil
}

// parseStatusUID reads the real user ID of a process from its
// /proc/<pid>/status, the first of the IDs on its "Uid:" line.
func parseStatusUID(data []byte) (int, error) {
	for line := range strings.Lines(string(data)) {
		rest, ok := strings.CutPrefix(line, "Uid:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			break
		}
		return strconv.Atoi(fields[0])
	}
	return 0, errors.New("status: no Uid")
}

// parseStatm reads how many pages of a process are resident from its
// /proc/<pid>/statm, which is the second field.
func parseStatm(data []byte) (uint64, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, errors.New("statm: too few fields")
	}
	return strconv.ParseUint(fields[1], 10, 64)
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
