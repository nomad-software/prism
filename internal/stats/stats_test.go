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

func TestProcessKey(t *testing.T) {
	p := Process{PID: 42, Start: 7, Name: "a", CPU: 1}
	if k := p.Key(); k != (ProcessKey{PID: 42, Start: 7}) {
		t.Errorf("Key = %+v", k)
	}
	// What changes as it runs does not change it, and a PID given to
	// another process later does.
	same := p
	same.Name, same.CPU, same.Memory = "b", 2, 3
	if same.Key() != p.Key() {
		t.Error("the key changed as the process ran")
	}
	later := p
	later.Start = 8
	if later.Key() == p.Key() {
		t.Error("a process given the PID later has the same key")
	}
}

func TestSignals(t *testing.T) {
	s := Signals()
	if len(s) == 0 || s[0].Name != "SIGKILL" || s[0].Number != 9 {
		t.Errorf("Signals = %+v, want SIGKILL first", s)
	}
}
