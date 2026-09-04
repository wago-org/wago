// Package hwcounters provides opt-in, thread-local execution hardware counters
// for benchmark hot regions. Platform implementations exclude kernel and
// hypervisor work and report multiplex timing with every count.
package hwcounters

import "errors"

var ErrUnsupported = errors.New("hardware counters are unsupported on this host")

type Count struct {
	Name        string
	Value       uint64
	TimeEnabled uint64
	TimeRunning uint64
}

// Scaled compensates for kernel multiplexing. A zero running time is invalid
// and returns zero rather than manufacturing an unmeasured count.
func (c Count) Scaled() float64 {
	if c.TimeRunning == 0 {
		return 0
	}
	return float64(c.Value) * float64(c.TimeEnabled) / float64(c.TimeRunning)
}
