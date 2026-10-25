//go:build unix

package stats

import "syscall"

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

// kill sends the signal of the number to the process with the PID.
func kill(pid, number int) error {
	return syscall.Kill(pid, syscall.Signal(number))
}
