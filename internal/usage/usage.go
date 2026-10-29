// Package usage works out what the counters stats reads say: how busy the
// processors and processes were, and how fast network adapters received and
// sent, between one reading and the next. It has no dependency on the UI.
package usage

import (
	"time"

	"github.com/nomad-software/prism/internal/stats"
)

// Busy returns the percentage of the time between two readings of a
// processor's times that it was busy. It is 0 where no time passed, or where
// the counters went back, as they do when a processor is brought online.
func Busy(before, after stats.CPUTimes) float64 {
	if after.Busy < before.Busy || after.Idle < before.Idle {
		return 0
	}
	total := after.Total() - before.Total()
	if total == 0 {
		return 0
	}
	return 100 * float64(after.Busy-before.Busy) / float64(total)
}

// CPU works out how busy the processors are from one reading to the next.
type CPU struct {
	last stats.CPU
	ok   bool
}

// Update takes a reading, and returns how busy all of the processors were
// together and each of them was since the last. It returns false for the
// first reading, which there is nothing to compare with.
func (c *CPU) Update(now stats.CPU) (all float64, cores []float64, ok bool) {
	last, had := c.last, c.ok
	c.last, c.ok = now, true
	if !had {
		return 0, nil, false
	}
	cores = make([]float64, len(now.Cores))
	for i, t := range now.Cores {
		if i < len(last.Cores) {
			cores[i] = Busy(last.Cores[i], t)
		}
	}
	return Busy(last.All, now.All), cores, true
}

// Rate is how fast an adapter received and sent, in bytes a second, and how
// much it has in all. The rates are 0 where it was not in the last reading,
// which they are worked out from.
type Rate struct {
	Received, Sent float64
	// Total is how much it has received and sent in all.
	Total stats.NetCounters
}

// Network works out how fast network adapters receive and send from one
// reading to the next.
type Network struct {
	last map[string]stats.NetCounters
	at   time.Time
}

// Update takes a reading made at the time, and returns the rate of each
// adapter in it. An adapter that was not in the last reading, as each is in
// the first, is not measured until the next. One whose counters went back,
// as they do when it is reset, has a rate of 0.
func (n *Network) Update(at time.Time, now map[string]stats.NetCounters) map[string]Rate {
	seconds := at.Sub(n.at).Seconds()
	rates := make(map[string]Rate, len(now))
	for name, c := range now {
		r := Rate{Total: c}
		if before, ok := n.last[name]; ok && seconds > 0 {
			r.Received = perSecond(before.Received, c.Received, seconds)
			r.Sent = perSecond(before.Sent, c.Sent, seconds)
		}
		rates[name] = r
	}
	n.last, n.at = now, at
	return rates
}

// perSecond returns how fast a counter went from before to after in the
// seconds, or 0 if it went back.
func perSecond(before, after uint64, seconds float64) float64 {
	if after < before {
		return 0
	}
	return float64(after-before) / seconds
}

// Process is a process, and how busy it was since the last reading.
type Process struct {
	stats.Process
	// Busy is the percentage of the time of one processor it spent
	// running, as top counts it, which is more than 100 for one running on
	// more than one at once, but no more than 100 for each there is.
	Busy float64
	// Measured says whether Busy is known, which it is from the process's
	// second reading, where it is not restricted.
	Measured bool
}

// Processes works out how busy processes are from one reading to the next.
type Processes struct {
	last map[stats.ProcessKey]time.Duration
	at   time.Time
}

// Update takes a reading made at the time, when there were the processors
// online, or 0 if that is not known, and returns each process in it with how
// busy it was since the last. A process that was not in the last reading, as
// none is in the first, is not measured until the next.
func (p *Processes) Update(at time.Time, now []stats.Process, processors int) []Process {
	seconds := at.Sub(p.at).Seconds()
	last := make(map[stats.ProcessKey]time.Duration, len(now))
	processes := make([]Process, len(now))
	for i, s := range now {
		key := s.Key()
		processes[i] = Process{Process: s}
		if s.Restricted {
			continue
		}
		last[key] = s.CPU
		if before, ok := p.last[key]; ok && seconds > 0 {
			processes[i].Measured = true
			if s.CPU > before {
				busy := 100 * (s.CPU - before).Seconds() / seconds
				// The counters can say a little more than the processors
				// had, at the edges of their ticks.
				if processors > 0 {
					busy = min(busy, 100*float64(processors))
				}
				processes[i].Busy = busy
			}
		}
	}
	p.last, p.at = last, at
	return processes
}
