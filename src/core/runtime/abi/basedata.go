// Package abi exposes runtime layout constants shared by native-code emitters
// and the runtime implementation.
package abi

// Basedata offsets are byte distances below the linear-memory base.
const (
	// FuncRefDescPtrOffset is the basedata slot holding the per-instance canonical
	// funcref descriptor array used by table.set/fill/grow/ref.func lowering.
	FuncRefDescPtrOffset = 88
	// FuncRefDescCountOffset is the u32 count for FuncRefDescPtrOffset.
	FuncRefDescCountOffset = 96
	// TrapCellPtrOffset is the basedata slot holding the address of the trap
	// cell ([linMem - TrapCellPtrOffset], u64), installed once per native entry
	// by the runtime. Only the cold trap path reads it — calls and returns carry
	// no trap protocol. Keep this at 104: signal/trampoline assembly references it.
	TrapCellPtrOffset = 104

	// GlobalsPtrOffset is the basedata slot holding the per-instance globals
	// slot-array pointer, addressed by JIT code as [linMem - GlobalsPtrOffset].
	GlobalsPtrOffset = 112

	// PassiveElemPtrOffset is the basedata slot holding passive element segment
	// descriptors for table.init/elem.drop.
	PassiveElemPtrOffset = 120

	// BasedataSize keeps the linear-memory base 16-byte aligned after the wago
	// extension fields appended to the WARP-compatible basedata layout.
	BasedataSize = 128
)
