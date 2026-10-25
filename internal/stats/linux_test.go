package stats

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
