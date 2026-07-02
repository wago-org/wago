// Package abi exposes runtime layout constants shared by native-code emitters
// and the runtime implementation.
package abi

// Basedata offsets are byte distances below the linear-memory base.
const (
	// GlobalsPtrOffset is the basedata slot holding the per-instance globals
	// slot-array pointer, addressed by JIT code as [linMem - GlobalsPtrOffset].
	GlobalsPtrOffset = 88

	// TrapCellPtrOffset is the basedata slot holding the address of the trap
	// cell ([linMem - TrapCellPtrOffset], u64), installed once per native entry
	// by the runtime. Only the cold trap path reads it — calls and returns carry
	// no trap protocol.
	TrapCellPtrOffset = 104

	// BasedataSize keeps the linear-memory base 16-byte aligned after the wago
	// extension fields appended to the WARP-compatible basedata layout.
	BasedataSize = 112
)
