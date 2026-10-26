// Package stats reads what the system says of its processors, memory,
// network adapters and processes: on Linux from /proc and /sys, and on macOS
// from the kernel's own interfaces. It reads counters as they are at one
// moment; working out how they change is left to the caller. It has no
// dependency on the UI.
package stats

import (
	"slices"
	"time"
)

// CPUTimes is the time a processor has spent, in ticks of the system's
// clock, since the system started.
type CPUTimes struct {
	// Busy is the time spent running anything, and Idle the time spent
	// with nothing to run, waiting for disks included.
	Busy, Idle uint64
}

// Total is all of the time counted.
func (t CPUTimes) Total() uint64 {
	return t.Busy + t.Idle
}

// CPU is the time spent by the processors.
type CPU struct {
	// All is the time of every processor together.
	All CPUTimes
	// Cores is the time of each logical processor (each thread of each
	// core), in the order the system numbers them.
	Cores []CPUTimes
}

// Memory is how much memory and swap there is, and is in use, in bytes.
type Memory struct {
	Total, Used         uint64
	SwapTotal, SwapUsed uint64
}

// NetCounters is how much a network adapter has received and sent, in
// bytes, since it appeared. The counters can start again from zero, as
// when the adapter is reset.
type NetCounters struct {
	Received, Sent uint64
}

// Process is what is read of a process.
type Process struct {
	PID int
	// PPID is the PID of the process that started it, or 0 if none did.
	PPID int
	// UID is the user it runs for, its real user ID.
	UID  int
	Name string
	// Start is when it started, in the system's own units, which tells it
	// from a process given the same PID after it has ended.
	Start uint64
	// CPU is the time it has spent running, in user and system time, of
	// every thread together.
	CPU time.Duration
	// Memory is how much memory it has, in bytes: on Linux what is resident,
	// and on macOS its footprint, as Activity Monitor counts it.
	Memory uint64
	// Restricted says that its time and memory could not be read, as those
	// of other users' processes cannot on macOS but by root.
	Restricted bool
	// Kernel says that it is a thread of the Linux kernel, which has no
	// memory of its own to count; what it uses is the kernel's.
	Kernel bool
}

// ProcessKey tells processes apart, as one that has ended does from one given
// its PID later.
type ProcessKey struct {
	PID   int
	Start uint64
}

// Key returns what tells the process apart from any other.
func (p Process) Key() ProcessKey {
	return ProcessKey{p.PID, p.Start}
}

// nobodyUIDs are the user IDs of nobody: 65534 on Linux, and -2 on macOS,
// which is unsigned there.
var nobodyUIDs = []int{65534, 1<<32 - 2}

// System says whether the process is the system's, rather than that of a
// person: a thread of the kernel, or one run for root, for an account of a
// service, below the first user ID people are given, or for nobody.
func (p Process) System() bool {
	return p.Kernel || p.UID < firstUserUID || slices.Contains(nobodyUIDs, p.UID)
}

// Source reads the statistics of the system, and signals its processes.
type Source interface {
	CPU() (CPU, error)
	Memory() (Memory, error)
	// Network returns the counters of every network adapter, by name.
	Network() (map[string]NetCounters, error)
	// Processes returns every process that could be read, in no order.
	Processes() ([]Process, error)
	// Signal sends the signal of the number to the process, if it is still
	// the one read, and not another given its PID since; ErrEnded if not.
	Signal(p Process, number int) error
}

// New returns the source of the system the program runs on.
func New() Source {
	return newSystem()
}
