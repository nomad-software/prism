package stats

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeFiles makes a root with the files, by their paths under it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, data := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const stat = `cpu  100 10 50 1000 40 5 5 2 30 0
cpu0 60 5 30 400 20 3 3 1 20 0
cpu1 40 5 20 600 20 2 2 1 10 0
intr 12345 0 0
ctxt 6789
btime 1700000000
processes 1234
`

func TestCPUTimesTotal(t *testing.T) {
	if got := (CPUTimes{Busy: 3, Idle: 4}).Total(); got != 7 {
		t.Errorf("Total = %d, want 7", got)
	}
}

func TestParseStat(t *testing.T) {
	c, err := parseStat(strings.NewReader(stat))
	if err != nil {
		t.Fatal(err)
	}
	// Busy is user, nice, system, irq, softirq and steal; idle is idle and
	// iowait; guest is left out.
	want := CPU{
		All: CPUTimes{Busy: 100 + 10 + 50 + 5 + 5 + 2, Idle: 1000 + 40},
		Cores: []CPUTimes{
			{Busy: 60 + 5 + 30 + 3 + 3 + 1, Idle: 400 + 20},
			{Busy: 40 + 5 + 20 + 2 + 2 + 1, Idle: 600 + 20},
		},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("parseStat = %+v, want %+v", c, want)
	}
}

func TestParseStatOldKernel(t *testing.T) {
	// Kernels before 2.6 have only four columns.
	c, err := parseStat(strings.NewReader("cpu 1 2 3 4\ncpu0 1 2 3 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := CPUTimes{Busy: 6, Idle: 4}
	if c.All != want || len(c.Cores) != 1 || c.Cores[0] != want {
		t.Errorf("parseStat = %+v, want All and one core of %+v", c, want)
	}
}

func TestParseStatOfflineCore(t *testing.T) {
	c, err := parseStat(strings.NewReader("cpu 2 0 0 2\ncpu0 1 0 0 1\ncpu2 1 0 0 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []CPUTimes{{Busy: 1, Idle: 1}, {}, {Busy: 1, Idle: 1}}
	if !reflect.DeepEqual(c.Cores, want) {
		t.Errorf("Cores = %+v, want %+v", c.Cores, want)
	}
}

func TestParseStatIgnoresOtherLines(t *testing.T) {
	c, err := parseStat(strings.NewReader("cpu 1 0 0 1\ncpux 1 2 3 4\ncpu 5\nsomething else\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Cores) != 0 {
		t.Errorf("Cores = %+v, want none", c.Cores)
	}
}

func TestParseStatErrors(t *testing.T) {
	for name, input := range map[string]string{
		"empty":     "",
		"no total":  "cpu0 1 2 3 4\n",
		"bad value": "cpu 1 2 x 4\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseStat(strings.NewReader(input)); err == nil {
				t.Error("parseStat succeeded, want an error")
			}
		})
	}
}

const meminfo = `MemTotal:       16000000 kB
MemFree:         2000000 kB
MemAvailable:   10000000 kB
Buffers:          500000 kB
Cached:          4000000 kB
SwapCached:            0 kB
SwapTotal:       8000000 kB
SwapFree:        6000000 kB
HugePages_Total:       0
`

func TestParseMeminfo(t *testing.T) {
	m, err := parseMeminfo(strings.NewReader(meminfo))
	if err != nil {
		t.Fatal(err)
	}
	want := Memory{
		Total:     16000000 * 1024,
		Used:      6000000 * 1024,
		SwapTotal: 8000000 * 1024,
		SwapUsed:  2000000 * 1024,
	}
	if m != want {
		t.Errorf("parseMeminfo = %+v, want %+v", m, want)
	}
}

func TestParseMeminfoWithoutAvailable(t *testing.T) {
	m, err := parseMeminfo(strings.NewReader("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 250 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := uint64(600 * 1024); m.Used != want {
		t.Errorf("Used = %d, want %d", m.Used, want)
	}
	if m.SwapTotal != 0 || m.SwapUsed != 0 {
		t.Errorf("swap = %d of %d, want none", m.SwapUsed, m.SwapTotal)
	}
}

func TestParseMeminfoNeverNegative(t *testing.T) {
	// The values are read at slightly different moments, and can disagree.
	m, err := parseMeminfo(strings.NewReader("MemTotal: 100 kB\nMemAvailable: 200 kB\nSwapTotal: 10 kB\nSwapFree: 20 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Used != 0 || m.SwapUsed != 0 {
		t.Errorf("Used = %d, SwapUsed = %d, want 0 and 0", m.Used, m.SwapUsed)
	}
}

func TestParseMeminfoErrors(t *testing.T) {
	if _, err := parseMeminfo(strings.NewReader("MemFree: 100 kB\nnonsense\nBad: x kB\nEmpty:\n")); err == nil {
		t.Error("parseMeminfo succeeded without MemTotal, want an error")
	}
}

func TestLinux(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"proc/stat":                                stat,
		"proc/meminfo":                             meminfo,
		"sys/class/net/lo/statistics/rx_bytes":     "100\n",
		"sys/class/net/lo/statistics/tx_bytes":     "100\n",
		"sys/class/net/eth0/statistics/rx_bytes":   "123456789\n",
		"sys/class/net/eth0/statistics/tx_bytes":   "987654\n",
		"sys/class/net/broken/statistics/rx_bytes": "nonsense\n",
		"sys/class/net/broken/statistics/tx_bytes": "1\n",
		"sys/class/net/half/statistics/rx_bytes":   "1\n",
	})
	l := Linux{Root: root}

	c, err := l.CPU()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Cores) != 2 {
		t.Errorf("CPU has %d cores, want 2", len(c.Cores))
	}

	m, err := l.Memory()
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 16000000*1024 {
		t.Errorf("Memory total = %d", m.Total)
	}

	n, err := l.Network()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]NetCounters{
		"lo":   {Received: 100, Sent: 100},
		"eth0": {Received: 123456789, Sent: 987654},
	}
	if !reflect.DeepEqual(n, want) {
		t.Errorf("Network = %+v, want %+v", n, want)
	}
}

func TestLinuxMissingFiles(t *testing.T) {
	l := Linux{Root: t.TempDir()}
	if _, err := l.CPU(); err == nil {
		t.Error("CPU succeeded without /proc/stat")
	}
	if _, err := l.Memory(); err == nil {
		t.Error("Memory succeeded without /proc/meminfo")
	}
	if _, err := l.Network(); err == nil {
		t.Error("Network succeeded without /sys/class/net")
	}
}

// TestSystem reads the statistics of the system the tests run on, which has
// at least one processor, some memory, and a network adapter (loopback, if
// nothing else).
func TestSystem(t *testing.T) {
	s := New()
	c, err := s.CPU()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Cores) == 0 || c.All.Total() == 0 {
		t.Errorf("CPU = %+v, want some processors that have run", c)
	}
	m, err := s.Memory()
	if err != nil {
		t.Fatal(err)
	}
	if m.Total == 0 || m.Used > m.Total || m.SwapUsed > m.SwapTotal {
		t.Errorf("Memory = %+v", m)
	}
	n, err := s.Network()
	if err != nil {
		t.Fatal(err)
	}
	if len(n) == 0 {
		t.Error("Network found no adapters")
	}
}

func TestParseLineTooLong(t *testing.T) {
	// A line longer than the scanner takes is an error, not a short read.
	long := "cpu " + strings.Repeat("1", 70_000) + "\n"
	if _, err := parseStat(strings.NewReader(long)); err == nil {
		t.Error("parseStat of an overlong line succeeded")
	}
	if _, err := parseMeminfo(strings.NewReader("MemTotal: " + long)); err == nil {
		t.Error("parseMeminfo of an overlong line succeeded")
	}
}

func TestParseProcessStat(t *testing.T) {
	// The name has spaces and brackets of its own.
	data := "1234 (a (b) c) S 1 1234 1234 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 1 0 98765 1000 200\n"
	p, err := parseProcessStat([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Process{Name: "a (b) c", PPID: 1, CPU: 3 * time.Second, Start: 98765}); p != want {
		t.Errorf("parseProcessStat = %+v, want %+v", p, want)
	}
	// A thread of the kernel has PF_KTHREAD among its flags.
	kernel := "2 (kthreadd) S 0 0 0 0 -1 2129984 0 0 0 0 0 0 0 0 20 0 1 0 5 0 0\n"
	if p, err := parseProcessStat([]byte(kernel)); err != nil || !p.Kernel {
		t.Errorf("parseProcessStat of a kernel thread = %+v, %v", p, err)
	}
	for _, bad := range []string{"", "1234 no name", "1234 (x) S 1 2", "1234 (x) S 1 2 3 4 5 6 7 8 9 10 x 0 0 0 0 0 0 0 1 0 0"} {
		if _, err := parseProcessStat([]byte(bad)); err == nil {
			t.Errorf("parseProcessStat(%q) succeeded", bad)
		}
	}
}

func TestParseStatusUID(t *testing.T) {
	status := "Name:\tbash\nUmask:\t0022\nState:\tS (sleeping)\nUid:\t1000\t0\t1000\t1000\nGid:\t100\t100\t100\t100\n"
	if uid, err := parseStatusUID([]byte(status)); err != nil || uid != 1000 {
		t.Errorf("parseStatusUID = %d, %v, want the real ID", uid, err)
	}
	for _, bad := range []string{"", "Name:\tx\n", "Uid:\n", "Uid:\tx\t0\n"} {
		if _, err := parseStatusUID([]byte(bad)); err == nil {
			t.Errorf("parseStatusUID(%q) succeeded", bad)
		}
	}
}

func TestParseStatm(t *testing.T) {
	if n, err := parseStatm([]byte("5000 1200 300 1 0 800 0\n")); err != nil || n != 1200 {
		t.Errorf("parseStatm = %d, %v", n, err)
	}
	for _, bad := range []string{"", "5000", "5000 x"} {
		if _, err := parseStatm([]byte(bad)); err == nil {
			t.Errorf("parseStatm(%q) succeeded", bad)
		}
	}
}

func TestLinuxProcesses(t *testing.T) {
	const fields = " S 1 1 1 0 -1 0 0 0 0 0 150 50 0 0 20 0 1 0 777 0 0\n"
	root := writeFiles(t, map[string]string{
		"proc/1/stat":       "1 (init)" + fields,
		"proc/1/statm":      "100 10 0 0 0 0 0\n",
		"proc/1/status":     "Name:\tinit\nUid:\t0\t0\t0\t0\n",
		"proc/6/stat":       "6 (bad statm)" + fields,
		"proc/6/statm":      "100\n",
		"proc/6/status":     "Uid:\t0\t0\t0\t0\n",
		"proc/7/stat":       "7 (bad status)" + fields,
		"proc/7/statm":      "100 10 0 0 0 0 0\n",
		"proc/7/status":     "Uid:\tnobody\n",
		"proc/4/stat":       "4 (no status)" + fields,
		"proc/4/statm":      "100 10 0 0 0 0 0\n",
		"proc/2/stat":       "2 (no statm)" + fields,
		"proc/3/stat":       "3 (broken) S\n",
		"proc/3/statm":      "100 10 0 0 0 0 0\n",
		"proc/self/stat":    "1 (init)" + fields,
		"proc/self/statm":   "100 10 0 0 0 0 0\n",
		"proc/meminfo":      meminfo,
		"proc/0/stat":       "0 (zero)" + fields,
		"proc/0/statm":      "100 10 0 0 0 0 0\n",
		"proc/-4/stat":      "-4 (negative)" + fields,
		"proc/-4/statm":     "100 10 0 0 0 0 0\n",
		"proc/10/somewhere": "",
	})
	l := Linux{Root: root}
	got, err := l.Processes()
	if err != nil {
		t.Fatal(err)
	}
	want := []Process{{PID: 1, PPID: 1, Name: "init", Start: 777, CPU: 2 * time.Second, Memory: 10 * uint64(os.Getpagesize())}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Processes = %+v, want %+v", got, want)
	}
	if _, err := (Linux{Root: t.TempDir()}).Processes(); err == nil {
		t.Error("Processes succeeded without /proc")
	}

	// A process that has ended, or whose PID is another's now, is not sent
	// the signal; nor is a PID that would be a group's. None of these
	// reaches a process of the system's.
	for _, p := range []Process{{PID: 1, Start: 778}, {PID: 3}, {PID: 5}, {PID: 0}, {PID: -1}} {
		if err := l.Signal(p, 0); !errors.Is(err, ErrEnded) {
			t.Errorf("Signal(%+v) = %v, want ErrEnded", p, err)
		}
	}
}

// TestSignalSelf sends signal 0, which checks that a process could be
// signalled without signalling it, to the tests' own process.
func TestSignalSelf(t *testing.T) {
	processes, err := New().Processes()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(processes, func(p Process) bool { return p.PID == os.Getpid() })
	if i < 0 {
		t.Fatal("the tests' own process is not listed")
	}
	self := processes[i]
	if self.Name == "" || self.Memory == 0 || self.Restricted || self.Kernel || self.UID != os.Getuid() || self.PPID != os.Getppid() {
		t.Errorf("own process = %+v", self)
	}
	if err := New().Signal(self, 0); err != nil {
		t.Errorf("Signal = %v", err)
	}
}
