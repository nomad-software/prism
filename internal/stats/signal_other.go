//go:build !unix

package stats

import "errors"

// Signals returns the signals a process can be sent, of which there are none
// on a system without them.
func Signals() []Signal {
	return nil
}

func kill(int, int) error {
	return errors.ErrUnsupported
}
