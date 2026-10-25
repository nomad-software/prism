//go:build !linux && !darwin

package stats

import "errors"

// firstUserUID is the first user ID people are taken to be given.
const firstUserUID = 1000

// unsupported is the source of a system there is no way to read yet: each of
// its statistics is an error, which is shown in place of them.
type unsupported struct{}

func newSystem() Source {
	return unsupported{}
}

func (unsupported) CPU() (CPU, error)       { return CPU{}, errors.ErrUnsupported }
func (unsupported) Memory() (Memory, error) { return Memory{}, errors.ErrUnsupported }
func (unsupported) Network() (map[string]NetCounters, error) {
	return nil, errors.ErrUnsupported
}
func (unsupported) Processes() ([]Process, error) { return nil, errors.ErrUnsupported }
func (unsupported) Signal(Process, int) error     { return errors.ErrUnsupported }
