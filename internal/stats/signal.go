package stats

import (
	"errors"
	"syscall"
)

// Signal is a signal a process can be sent.
type Signal struct {
	// Name is what it is called, as "SIGKILL".
	Name string
	// Number is its number on the system the program runs on.
	Number int
}

// ErrEnded is the error of signalling a process that has ended, or whose PID
// has been given to another.
var ErrEnded = errors.New("the process has ended")

// signal sends the signal of the number to the process, if started says it
// is still running and started when it did.
func signal(p Process, started func(pid int) (uint64, bool), number int) error {
	// A PID of 0 or less would signal a group of processes, not one.
	if p.PID <= 0 {
		return ErrEnded
	}
	if start, ok := started(p.PID); !ok || start != p.Start {
		return ErrEnded
	}
	return syscall.Kill(p.PID, syscall.Signal(number))
}

// Signals returns the signals a process can be sent, the first of them,
// SIGKILL, the one sent unless another is chosen.
func Signals() []Signal {
	return []Signal{
		{"SIGKILL", int(syscall.SIGKILL)},
		{"SIGTERM", int(syscall.SIGTERM)},
		{"SIGINT", int(syscall.SIGINT)},
		{"SIGHUP", int(syscall.SIGHUP)},
		{"SIGQUIT", int(syscall.SIGQUIT)},
		{"SIGSTOP", int(syscall.SIGSTOP)},
		{"SIGCONT", int(syscall.SIGCONT)},
		{"SIGUSR1", int(syscall.SIGUSR1)},
		{"SIGUSR2", int(syscall.SIGUSR2)},
	}
}
