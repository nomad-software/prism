//go:build !linux && !darwin

package stats

import "errors"

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
