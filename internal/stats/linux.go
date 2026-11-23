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
	"syscall"
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
// read, is left out. Each adapter there is a link to its device, and those
// the system made up are kept apart from the computer's own, under
// /sys/devices/virtual.
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
		device, _ := os.Readlink(filepath.Join(dir, e.Name()))
		counters[e.Name()] = NetCounters{
			Received: received,
			Sent:     sent,
			Virtual:  strings.Contains(filepath.ToSlash(device), "/devices/virtual/"),
		}
	}
	return counters, nil
}

// userHZ is how many ticks a second the times of processes in /proc are
// counted in: USER_HZ, which Linux keeps at 100 for every program, whatever
// its own clock runs at.
const userHZ = 100

// Processes reads /proc/<pid>/stat and /proc/<pid>/statm of every process,
// and /proc/<pid>/cmdline of one whose name the kernel cut short; the user
// each runs for is the owner of its folder. A process that cannot be read,
// as one that ended while the others were being read, is left out.
func (l Linux) Processes() ([]Process, error) {
	dir := filepath.Join(l.Root, "proc")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	page := uint64(os.Getpagesize())
	processes := make([]Process, 0, len(entries))
	// Every file is read into the one buffer, which holds the whole of any
	// but a long cmdline, of which the start is all that is wanted.
	buf := make([]byte, 4096)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if p, err := readProcess(filepath.Join(dir, e.Name()), buf); err == nil {
			p.PID, p.Memory = pid, p.Memory*page
			processes = append(processes, p)
		}
	}
	return processes, nil
}

// readProcess reads the process whose folder in /proc is at dir, all but its
// PID, with its memory in pages, using buf to read its files into.
func readProcess(dir string, buf []byte) (Process, error) {
	data, err := readInto(dir+"/stat", buf)
	if err != nil {
		return Process{}, err
	}
	p, err := parseProcessStat(data)
	if err != nil {
		return Process{}, err
	}
	if data, err = readInto(dir+"/statm", buf); err != nil {
		return Process{}, err
	}
	if p.Memory, err = parseStatm(data); err != nil {
		return Process{}, err
	}
	// The kernel has the folder owned by the user the process runs as, its
	// effective user ID, which costs far less to ask for than reading the
	// IDs from /proc/<pid>/status, which the kernel writes out in full.
	var owner syscall.Stat_t
	if err := syscall.Stat(dir, &owner); err != nil {
		return Process{}, err
	}
	p.UID = int(owner.Uid)
	if len(p.Name) == commLength {
		if data, err := readInto(dir+"/cmdline", buf); err == nil {
			p.Name = fullName(p.Name, data)
		}
	}
	return p, nil
}

// readInto reads the file at path into buf, and returns what was read: all
// of the file, or as much as buf holds. It asks the system directly, as
// thousands of these files are read at a time, and opening each as an
// os.File and measuring it first costs more than reading it.
func readInto(path string, buf []byte) ([]byte, error) {
	var fd int
	for {
		var err error
		if fd, err = syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0); err == nil {
			break
		} else if err != syscall.EINTR {
			return nil, err
		}
	}
	defer syscall.Close(fd)
	n := 0
	for n < len(buf) {
		m, err := syscall.Read(fd, buf[n:])
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		if m == 0 {
			break
		}
		n += m
	}
	return buf[:n], nil
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
	left, right := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if left < 0 || right < left {
		return Process{}, errors.New("stat: no name")
	}
	// The fields are read as they are passed, without a string made of the
	// line or of each of them.
	var ppid, flags, utime, stime, starttime uint64
	rest := data[right+1:]
	for _, value := range [...]*uint64{1: &ppid, 6: &flags, 11: &utime, 12: &stime, 19: &starttime} {
		var field []byte
		if field, rest = nextField(rest); len(field) == 0 {
			return Process{}, errors.New("stat: too few fields")
		}
		if value == nil {
			continue
		}
		v, err := strconv.ParseUint(string(field), 10, 64)
		if err != nil {
			return Process{}, fmt.Errorf("stat: %w", err)
		}
		*value = v
	}
	return Process{
		Name:   string(data[left+1 : right]),
		CPU:    time.Duration(utime+stime) * (time.Second / userHZ),
		Start:  starttime,
		Kernel: flags&pfKthread != 0,
		PPID:   int(ppid),
	}, nil
}

// nextField returns the first of the fields of data, which spaces, tabs and
// new lines are between, and what follows it. The field is empty if there is
// none.
func nextField(data []byte) (field, rest []byte) {
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }
	i := 0
	for i < len(data) && space(data[i]) {
		i++
	}
	j := i
	for j < len(data) && !space(data[j]) {
		j++
	}
	return data[i:j], data[j:]
}

// commLength is the longest name the kernel keeps of a process, in
// /proc/<pid>/stat: TASK_COMM_LEN, 16, less its closing NUL. A longer one is
// cut short to it.
const commLength = 15

// fullName returns the whole name of a process whose name the kernel cut
// short, from its /proc/<pid>/cmdline: the file name of the program it ran,
// its first argument, if that starts with the name the kernel kept. A program
// that rewrote its arguments into one line has its name taken from before the
// first space, if its path has none ("/opt/app/app --type=x /a/b"), and after
// a status of its own is cut off ("sshd: user [priv]"), less a colon at its
// end. If the first argument does not start with the name kept, as of a
// thread of the kernel, which has none, or a program that renamed itself, the
// name kept is returned.
func fullName(name string, cmdline []byte) string {
	first, _, _ := bytes.Cut(cmdline, []byte{0})
	full := filepath.Base(string(first))
	if !strings.HasPrefix(full, name) {
		program, _, _ := strings.Cut(string(first), " ")
		if full = filepath.Base(program); !strings.HasPrefix(full, name) {
			return name
		}
	}
	rest := full[len(name):]
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		rest = rest[:i]
	}
	return name + strings.TrimSuffix(rest, ":")
}

// parseStatm reads how many pages of a process are resident from its
// /proc/<pid>/statm, which is the second field.
func parseStatm(data []byte) (uint64, error) {
	_, rest := nextField(data)
	resident, _ := nextField(rest)
	if len(resident) == 0 {
		return 0, errors.New("statm: too few fields")
	}
	return strconv.ParseUint(string(resident), 10, 64)
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
