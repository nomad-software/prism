package usage

import (
	"reflect"
	"testing"
	"time"

	"github.com/nomad-software/prism/internal/stats"
)

func TestBusy(t *testing.T) {
	for _, c := range []struct {
		name          string
		before, after stats.CPUTimes
		want          float64
	}{
		{"half", stats.CPUTimes{Busy: 100, Idle: 100}, stats.CPUTimes{Busy: 150, Idle: 150}, 50},
		{"all", stats.CPUTimes{Busy: 0, Idle: 0}, stats.CPUTimes{Busy: 10, Idle: 0}, 100},
		{"none", stats.CPUTimes{Busy: 5, Idle: 5}, stats.CPUTimes{Busy: 5, Idle: 25}, 0},
		{"no time", stats.CPUTimes{Busy: 5, Idle: 5}, stats.CPUTimes{Busy: 5, Idle: 5}, 0},
		{"busy back", stats.CPUTimes{Busy: 50, Idle: 5}, stats.CPUTimes{Busy: 5, Idle: 50}, 0},
		{"idle back", stats.CPUTimes{Busy: 5, Idle: 50}, stats.CPUTimes{Busy: 50, Idle: 5}, 0},
	} {
		if got := Busy(c.before, c.after); got != c.want {
			t.Errorf("%s: Busy = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCPU(t *testing.T) {
	var c CPU
	if _, _, ok := c.Update(stats.CPU{All: stats.CPUTimes{Busy: 10, Idle: 10}}); ok {
		t.Error("the first Update is ok, with nothing to compare it with")
	}
	all, cores, ok := c.Update(stats.CPU{
		All:   stats.CPUTimes{Busy: 20, Idle: 20},
		Cores: []stats.CPUTimes{{Busy: 5, Idle: 5}},
	})
	if !ok || all != 50 {
		t.Errorf("Update = %v, %v; want 50, true", all, ok)
	}
	// A core that was not in the last reading has no use yet.
	if !reflect.DeepEqual(cores, []float64{0}) {
		t.Errorf("cores = %v, want [0]", cores)
	}
	_, cores, _ = c.Update(stats.CPU{
		All:   stats.CPUTimes{Busy: 30, Idle: 30},
		Cores: []stats.CPUTimes{{Busy: 8, Idle: 6}},
	})
	if !reflect.DeepEqual(cores, []float64{75}) {
		t.Errorf("cores = %v, want [75]", cores)
	}
}

func TestNetwork(t *testing.T) {
	var n Network
	start := time.Unix(1_700_000_000, 0)
	first := n.Update(start, map[string]stats.NetCounters{"eth0": {Received: 1000, Sent: 100}})
	want := map[string]Rate{"eth0": {Total: stats.NetCounters{Received: 1000, Sent: 100}}}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("first Update = %+v, want %+v", first, want)
	}

	second := n.Update(start.Add(2*time.Second), map[string]stats.NetCounters{
		"eth0":  {Received: 3000, Sent: 50},
		"wlan0": {Received: 7, Sent: 7},
	})
	want = map[string]Rate{
		// Sent went back, as when the adapter is reset.
		"eth0":  {Received: 1000, Sent: 0, Total: stats.NetCounters{Received: 3000, Sent: 50}},
		"wlan0": {Total: stats.NetCounters{Received: 7, Sent: 7}},
	}
	if !reflect.DeepEqual(second, want) {
		t.Errorf("second Update = %+v, want %+v", second, want)
	}

	// No time passing gives no rate rather than dividing by zero.
	third := n.Update(start.Add(2*time.Second), map[string]stats.NetCounters{"eth0": {Received: 4000, Sent: 60}})
	if r := third["eth0"]; r.Received != 0 || r.Sent != 0 {
		t.Errorf("Update with no time passed = %+v", r)
	}
}
