//go:build !linux || (!amd64 && !arm64) || tinygo || wago_target_tinygo

package runtime

import "time"

type interruptActivation struct{}

func beginInterruptActivation([]byte) (*interruptActivation, error) { return nil, nil }
func (*interruptActivation) enterWasm(uintptr)                      {}
func (*interruptActivation) leaveWasm()                             {}
func (*interruptActivation) close()                                 {}

func registerExecutableCode([]byte) error { return nil }
func unregisterExecutableCode([]byte)     {}

// RequestInterrupt publishes an interruption for cooperative native safepoints.
func RequestInterrupt(trap []byte) {
	storeTrap(trap, uint32(TrapInterrupted))
}

func RequestInterruptAsync(trap []byte) func() {
	RequestInterrupt(trap)
	return func() {}
}
func SetInterruptDeadline([]byte, time.Time) (func(), error) { return func() {}, nil }

// HostInterruptSupported reports whether this build can asynchronously unwind
// generated Wasm without compiler-inserted safepoints.
func HostInterruptSupported() bool { return false }
