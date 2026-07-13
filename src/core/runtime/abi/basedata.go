// Package abi exposes runtime layout constants shared by native-code emitters
// and the runtime implementation.
package abi

// Basedata offsets are byte distances below the linear-memory base.
const (
	// FuncRefDescPtrOffset is the basedata slot holding the per-instance canonical
	// funcref descriptor array used by table.set/fill/grow/ref.func lowering.
	FuncRefDescPtrOffset = 88
	// TableDirPtrOffset points at an array of table-descriptor pointers indexed by
	// the Wasm table index. Table index 0 continues to use the direct legacy slot
	// at offset 80; native code reads this directory only for nonzero indexes. This
	// reuses the former unused funcref-descriptor count slot; exact descriptor
	// validation is instance-owned and never read that basedata count.
	TableDirPtrOffset = 96
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

	// PassiveDataPtrOffset is the basedata slot holding the per-instance passive
	// data descriptor array used by memory.init/data.drop. Each descriptor is
	// {ptr u64, len u32, pad u32}; data.drop zeroes len.
	PassiveDataPtrOffset = 128

	// ImportBindingsPtrOffset points at the per-instance function-import binding
	// descriptor array used by the shared-image import-thunk path. A zero pointer
	// means every import follows the ordinary host path.
	ImportBindingsPtrOffset = 136

	// ImportBindingBytes is the size of one function-import binding descriptor:
	// {callee linear-memory base u64, wrapper-entry address u64}.
	ImportBindingBytes = 16

	// BasedataSize keeps the linear-memory base 16-byte aligned after the wago
	// extension fields appended to the WARP-compatible basedata layout.
	BasedataSize = 144
)
