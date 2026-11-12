package ui

import (
	"testing"
	"time"

	"github.com/nomad-software/qute/qutetest"

	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
)

// The benchmarks measure what the window does over and over while it runs:
// a frame of the graphs between readings, a reading of every statistic with
// the graphs and legends drawn again, a refresh of the processes, and the
// whole window drawn. Each runs its loop on the GUI thread, so that what is
// timed is the window's work and Qt's, and not passing calls to the thread.
// Run them with make bench.

// benchCores and benchAdapters are how many lines the graphs have: a
// machine of 16 processors, and 4 network adapters.
const (
	benchCores    = 16
	benchAdapters = 4
	benchProcs    = 600
)

// benchReadings has the fake source of the window give made up readings,
// a step at a time, of busy processors, memory in use and traffic.
type benchReadings struct {
	busy, idle     [benchCores]uint64
	received, sent uint64
	step           int
}

// next puts the next readings in the source.
func (r *benchReadings) next(f *fakeSource) {
	f.cpu = stats.CPU{Cores: make([]stats.CPUTimes, benchCores)}
	for i := range benchCores {
		b := uint64(10 + (r.step*7+i*13)%80)
		r.busy[i] += b
		r.idle[i] += 100 - b
		f.cpu.Cores[i] = stats.CPUTimes{Busy: r.busy[i], Idle: r.idle[i]}
		f.cpu.All.Busy += r.busy[i]
		f.cpu.All.Idle += r.idle[i]
	}
	f.memory = stats.Memory{Total: 32 << 30, Used: uint64(12<<30 + (r.step%50)*(40<<20)), SwapTotal: 8 << 30, SwapUsed: uint64(r.step%20) << 20}
	r.received += uint64(100<<10 + (r.step%9)*(150<<10))
	r.sent += uint64(10<<10 + (r.step%5)*(30<<10))
	f.network = map[string]stats.NetCounters{}
	for i, name := range []string{"enp5s0", "wlan0", "docker0", "lo"}[:benchAdapters] {
		f.network[name] = stats.NetCounters{Received: r.received / uint64(i+1), Sent: r.sent / uint64(i+1)}
	}
	r.step++
}

// sample reads every statistic of the graphs once.
func (tw *testWindow) sample() {
	tw.sampleCPU()
	tw.sampleMemory()
	tw.sampleNetwork()
}

// paintGraphs draws every graph now, rather than when the event loop next
// gets to it.
func (tw *testWindow) paintGraphs() {
	for _, g := range tw.graphs() {
		g.Repaint()
	}
}

// newBenchWindow returns a window of the graphs with a minute of readings,
// one a second.
func newBenchWindow(b *testing.B) (*testWindow, *benchReadings) {
	tw := newTestWindow(b, func(s *settings.Settings) { s.Intervals.Network = 1 })
	r := &benchReadings{}
	for range 61 {
		tw.source.set(r.next)
		qutetest.Run(b, tw.sample)
		tw.advance(time.Second)
	}
	qutetest.Run(b, tw.paintGraphs)
	return tw, r
}

// BenchmarkFrame times a frame between readings: every graph moved along by
// a pixel and drawn.
func BenchmarkFrame(b *testing.B) {
	tw, _ := newBenchWindow(b)
	qutetest.Run(b, func() {
		step := tw.cpu.graph.pixelTime()
		for b.Loop() {
			tw.advance(step)
			tw.animate()
			tw.paintGraphs()
		}
	})
}

// BenchmarkReading times a reading of the processors, memory and network,
// with their graphs and legends drawn.
func BenchmarkReading(b *testing.B) {
	tw, r := newBenchWindow(b)
	qutetest.Run(b, func() {
		for b.Loop() {
			tw.advance(time.Second)
			tw.source.set(r.next)
			tw.sample()
			tw.paintGraphs()
			tw.cpu.legend.Repaint()
			tw.memory.legend.Repaint()
			tw.network.legend.Repaint()
		}
	})
}

// BenchmarkWindow times drawing the whole window, its graphs drawn from
// their caches.
func BenchmarkWindow(b *testing.B) {
	tw, _ := newBenchWindow(b)
	qutetest.Run(b, func() {
		for b.Loop() {
			tw.Grab()
		}
	})
}

// benchProcesses returns a family of processes, each started by one before
// it, with the CPU time of the step.
func benchProcesses(step int) []stats.Process {
	list := make([]stats.Process, benchProcs)
	for i := range list {
		pid := i + 1
		list[i] = stats.Process{
			PID:    pid,
			PPID:   pid / 4,
			UID:    1000,
			Name:   []string{"firefox", "bash", "kworker/3:1", "systemd", "code", "prism"}[i%6],
			Start:  uint64(pid),
			CPU:    time.Duration(step*(i%37)) * time.Millisecond,
			Memory: uint64(i%50) << 20,
		}
	}
	return list
}

// benchmarkProcesses times a refresh of the processes, shown in the view.
func benchmarkProcesses(b *testing.B, view string) {
	tw := newTestWindow(b, func(s *settings.Settings) {
		s.ProcessFilter = settings.ProcessesAll
		s.ProcessView = view
	})
	step := 0
	qutetest.Run(b, func() {
		tw.tabs.SetCurrentIndex(1)
		for b.Loop() {
			step++
			tw.source.set(func(f *fakeSource) { f.procs = benchProcesses(step) })
			tw.advance(2 * time.Second)
			tw.sampleProcesses()
			tw.processes.tree.Viewport().Repaint()
		}
	})
}

func BenchmarkProcessList(b *testing.B) { benchmarkProcesses(b, settings.ViewList) }
func BenchmarkProcessTree(b *testing.B) { benchmarkProcesses(b, settings.ViewTree) }
