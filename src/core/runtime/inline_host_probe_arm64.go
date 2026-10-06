//go:build wago_inline_host_experiment && (linux || darwin) && arm64 && !tinygo

package runtime

// Experimental only: this entry owns a real Go frame while native code is
// active. The native bridge returns to that frame for a register-ABI Go call.
// Production dispatch does not use these symbols.
func inlineHostProbe(fn func(uint64) uint64, code, args, linMem, results, stack uintptr)

func inlineHostProbeBridgeAddr() uintptr
