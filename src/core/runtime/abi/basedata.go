//go:build linux && amd64

// Package abi exposes runtime layout constants shared by native-code emitters
// and the runtime implementation.
package abi

// Basedata offsets are byte distances below the linear-memory base.
const (
	// GlobalsPtrOffset is the basedata slot holding the per-instance globals
	// slot-array pointer, addressed by JIT code as [linMem - GlobalsPtrOffset].
	GlobalsPtrOffset = 88

	// BasedataSize keeps the linear-memory base 16-byte aligned after the wago
	// extension fields appended to the WARP-compatible basedata layout.
	BasedataSize = 96
)
