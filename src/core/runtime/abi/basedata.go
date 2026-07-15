// Package abi exposes runtime layout constants shared by native-code emitters
// and the runtime implementation.
package abi

// Basedata offsets are byte distances below the linear-memory base.
const (
	// MemoryDirPtrOffset points at 16-byte indexed-memory entries
	// {base u64, current-bytes u32, current-pages u32}. Memory 0 keeps the direct
	// RBX/basedata hot path; native code consults this directory only for nonzero
	// indexes. Offset 64 was the unused WARP memory-helper slot.
	MemoryDirPtrOffset = 64
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

	// ImportDispatchPtrOffset points at per-instance imported-function targets.
	// Entry size/field offsets are runtime.ImportDispatch* constants; entries are
	// ordered by Wasm function-import index.
	ImportDispatchPtrOffset = 136

	// TailArgsOffset is the end of a fixed 16-slot scratch bank used only while a
	// wrapper-ABI tail call tears down the current frame and enters the next one.
	// The bank occupies [linMem-272, linMem-144), immediately below the import-
	// dispatch pointer, and is reused by every tail step without allocation.
	TailArgsOffset = 272
	TailArgsSlots  = 16

	// TailCrossCodeOffset, TailCrossHomeOffset, and TailCrossContextOffset are
	// scratch slots at the high end of the wrapper-tail bank. A register-ABI
	// return_call_ref uses them only while transferring a root adapter or one fixed
	// nested return context into a retained cross-instance wrapper; wrapper-tail
	// and cross-tail contexts are mutually exclusive.
	TailCrossCodeOffset    = 152
	TailCrossHomeOffset    = 160
	TailCrossContextOffset = 168

	// FuncRefInternalHomeTag marks a descriptor whose code pointer is an internal
	// register-ABI entry in the same instance. FuncRefCrossInstanceHomeTag marks a
	// retained InstanceExport wrapper descriptor admitted by the bounded root or
	// nested-tail context transfer. FuncRefLocalWrapperHomeTag distinguishes a
	// same-instance wrapper descriptor from a host thunk that shares the caller's
	// basedata. The low 61 bits remain the canonical home linear-memory pointer on
	// supported linux/amd64 hosts.
	FuncRefInternalHomeTag      uint64 = 1 << 63
	FuncRefCrossInstanceHomeTag uint64 = 1 << 62
	FuncRefLocalWrapperHomeTag  uint64 = 1 << 61

	// BasedataSize keeps the linear-memory base 16-byte aligned after the wago
	// extension fields and the bounded wrapper-tail argument bank.
	BasedataSize = TailArgsOffset
)
