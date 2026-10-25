package stats

import "testing"

func TestSystemProcess(t *testing.T) {
	for _, c := range []struct {
		p    Process
		want bool
	}{
		{Process{UID: 0}, true},
		{Process{UID: firstUserUID - 1}, true},
		{Process{UID: firstUserUID}, false},
		{Process{UID: 60001}, false},
		{Process{UID: 65534}, true},
		{Process{UID: 1<<32 - 2}, true},
		{Process{UID: firstUserUID, Kernel: true}, true},
	} {
		if got := c.p.System(); got != c.want {
			t.Errorf("%+v System = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestSignals(t *testing.T) {
	s := Signals()
	if len(s) == 0 || s[0].Name != "SIGKILL" || s[0].Number != 9 {
		t.Errorf("Signals = %+v, want SIGKILL first", s)
	}
}
