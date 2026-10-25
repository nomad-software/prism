// Package stats reads what the system says of its processors, memory and
// network adapters: on Linux from /proc and /sys, and on macOS from the
// kernel's own interfaces. It reads counters as they are at one moment;
// working out how they change is left to the caller. It has no dependency on
// the UI.
package stats

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

// Source reads the statistics of the system.
type Source interface {
	CPU() (CPU, error)
	Memory() (Memory, error)
	// Network returns the counters of every network adapter, by name.
	Network() (map[string]NetCounters, error)
}

// New returns the source of the system the program runs on.
func New() Source {
	return newSystem()
}
