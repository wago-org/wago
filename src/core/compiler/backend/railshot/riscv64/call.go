//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const callFrameHeader = 48
const maxDirectCalls = 1024

type directCall struct {
	site        int
	callee      uint32
	helper      uint32
	helperSlots int
	safepoint   uint32
}

const (
	gcStructDispatchBit  uint32 = 1 << 30
	gcStructAllocDefault uint32 = 1
	gcStructGet          uint32 = 2
	gcStructSet          uint32 = 3
	gcStructGetS         uint32 = 4
	gcStructGetU         uint32 = 5
	gcStructRefTest      uint32 = 6
	gcStructTableSet     uint32 = 7
	gcAnyConvertExtern   uint32 = 8
	gcExternConvertAny   uint32 = 9
	gcStructRefCast      uint32 = 10
	gcStructAllocOne     uint32 = 11
	gcArrayAllocDefault  uint32 = 16
	gcArrayGet           uint32 = 17
	gcArrayGetS          uint32 = 18
	gcArrayGetU          uint32 = 19
	gcArraySet           uint32 = 20
	gcArrayLen           uint32 = 21
	gcArrayAllocFixed    uint32 = 22
	gcArrayAllocUniform  uint32 = 23
	gcArrayAllocData     uint32 = 24
	gcArrayAllocElem     uint32 = 25
	gcArrayDropElem      uint32 = 26
	gcArrayFill          uint32 = 27
	gcArrayCopy          uint32 = 28
	gcArrayInitData      uint32 = 29
	gcArrayInitElem      uint32 = 30
	// guestGlobalClear is an internal relocation key, not a GC helper ID.
	guestGlobalClear        uint32 = ^uint32(0)
	guestGlobalStore        uint32 = ^uint32(0) - 1
	guestGlobalStoreIndexed uint32 = ^uint32(0) - 2
	// guestGlobalStoreDispatch matches the permanently invalid zero-index
	// HostFuncRef dispatch encoding reserved by the runtime bridge.
	guestGlobalStoreDispatch uint32 = 1 << 31
)

// gcAllocHelperSlots returns the synchronous arena arity for a helper that can
// carry a nonzero allocation safepoint. Fixed-array and explicit struct
// allocation shapes retain the already admitted per-call arity.
func gcAllocHelperSlots(helper uint32, admitted int) (int, bool) {
	switch helper {
	case gcStructAllocDefault:
		return 1, true
	case gcStructAllocOne:
		return admitted, admitted == 2 || admitted == 4
	case gcArrayAllocDefault:
		return 2, true
	case gcArrayAllocUniform:
		return 3, true
	case gcArrayAllocFixed:
		return admitted, admitted == 4 || admitted == 5
	case gcArrayAllocData, gcArrayAllocElem:
		return 4, true
	default:
		return 0, false
	}
}

// Shared callees can allocate A0..A3. Caller homes and this saved header are
// authoritative across calls; X24 is reserved and unchanged in this same-instance
// ABI. Every callee consumes the shared serialized arena before it can nest.
func (f *integerEmitter) restoreCallTransport() {
	f.a.Ld(enc.X10, enc.SP, 16)
	if f.mode.production {
		f.a.Ld(enc.X13, enc.SP, 8)
		f.a.Ld(enc.X11, enc.SP, 32)
		f.a.Ld(enc.X12, enc.SP, 24)
	} else {
		f.a.Ld(enc.X11, enc.SP, 8)
		f.a.Ld(enc.X12, enc.SP, 24)
		f.a.Ld(enc.X13, enc.SP, 32)
	}
}

// clearGuestGlobal calls the image-local parked ownership bridge. The exact
// source classifier proves that the sole operand is ref.null func and that the
// destination is imported mutable global zero, so no raw reference crosses the
// Go boundary.
func (f *integerEmitter) clearGuestGlobal() {
	f.restoreCallTransport()
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: guestGlobalClear})
	f.restoreCallTransport()
}

// storeGuestGlobal transfers one authenticated full-width descriptor through
// the same parked ownership bridge. The exact source profile proves that the
// descriptor was produced by ref.func 0 in this image.
func (f *integerEmitter) storeGuestGlobal(slot int) {
	f.storeGuestGlobalAt(f.SpillOffset(slot))
}

// storeGuestGlobalAt is shared with the SWAR lowering, whose padded v128
// homes use a different spill function even when the stored value is one
// full-width funcref descriptor.
func (f *integerEmitter) storeGuestGlobalAt(offset int32) {
	f.restoreCallTransport()
	f.Load(5, offset, true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: guestGlobalStore})
	f.restoreCallTransport()
}

// storeGuestGlobalIndexed transfers one authenticated descriptor and its exact
// destination index through the parked bridge. Only the two-function exact
// bidirectional-copy profile can emit this relocation.
func (f *integerEmitter) storeGuestGlobalIndexed(slot int, index uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(slot), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X5, enc.Zero, int32(index))
	f.a.Sd(enc.X5, enc.X10, 8)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: guestGlobalStoreIndexed})
	f.restoreCallTransport()
}

func (f *integerEmitter) directCall(index uint32, sig functionSpec, base, importedFunctions int) {
	imported := index < uint32(importedFunctions)
	if imported {
		f.importCallTarget(index)
	}
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		// i32 loads are already zero-normalized; fill the complete arena cell.
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	if imported {
		f.a.Jalr(enc.RA, enc.X28, 0)
	} else {
		f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), callee: index - uint32(importedFunctions)})
	}
	f.restoreCallTransport()
	if !f.mode.production {
		f.a.Lwu(enc.X7, enc.X12, 0)
		f.propagate = append(f.propagate, f.a.BranchLong(enc.NE, enc.X7, enc.Zero))
	}
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	for j, wide := range sig.results {
		if wide {
			f.a.Ld(enc.X5, result, int32(j*8))
		} else {
			f.a.Lwu(enc.X5, result, int32(j*8))
		}
		f.Store(f.SpillOffset(base+j), 5, wide)
	}
}

// gcDefaultAllocation calls an exact struct or array default-allocation helper.
// The helper thunk uses the same checked parked-host protocol as a returning
// import, but is image-local and carries the reserved GC dispatch identity.
func (f *integerEmitter) gcDefaultAllocation(helper uint32, base int, array bool, typeIndex uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	if array {
		f.Load(5, f.SpillOffset(base), false)
		f.a.Sd(enc.X5, enc.X10, 0) // length
		f.a.Constant32(enc.X5, typeIndex)
		f.a.Sd(enc.X5, enc.X10, 8) // type index
	} else {
		f.a.Constant32(enc.X5, typeIndex)
		f.a.Sd(enc.X5, enc.X10, 0) // type index
	}
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: helper, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

// gcStructAllocation calls the exact struct.new helper with count narrow
// scalar fields followed by the type index. References remain full-width in
// the returned carrier.
func (f *integerEmitter) gcStructAllocation(base int, typeIndex, count uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	for i := uint32(0); i < count; i++ {
		f.Load(5, f.SpillOffset(base+int(i)), false)
		f.a.Sd(enc.X5, enc.X10, int32(i*8))
	}
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, int32(count*8))
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructAllocOne, helperSlots: int(count) + 1, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

// gcUniformArrayAllocation calls the exact pointer-free array.new helper. Both
// i32 operands are zero-normalized before filling their complete arena slots.
func (f *integerEmitter) gcUniformArrayAllocation(base int, typeIndex uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0) // uniform value
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // length
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16) // type index
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocUniform, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) gcFixedArrayAllocation(base int, typeIndex, count uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	for i := uint32(0); i < count; i++ {
		f.Load(5, f.SpillOffset(base+int(i)), false)
		f.a.Sd(enc.X5, enc.X10, int32(i*8))
	}
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, int32(count*8))
	f.a.Constant32(enc.X5, count)
	f.a.Sd(enc.X5, enc.X10, int32((count+1)*8))
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocFixed, helperSlots: int(count) + 2, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

// The official mutable f32 fixture leaves its public index below three fixed
// initializer operands. Allocate from the first pointer-free value, then write
// the remaining two values without another safepoint. This keeps the helper
// exchange bounded while preserving array.new_fixed operand order exactly.
func (f *integerEmitter) gcFixedArrayAllocation3ViaUniform(base int, typeIndex uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.a.Constant32(enc.X5, 3)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocUniform, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
	for index := 1; index < 3; index++ {
		f.restoreCallTransport()
		f.Load(5, f.SpillOffset(base), true)
		f.a.Sd(enc.X5, enc.X10, 0)
		f.a.Constant32(enc.X5, uint32(index))
		f.a.Sd(enc.X5, enc.X10, 8)
		f.Load(5, f.SpillOffset(base+index), false)
		f.a.Sd(enc.X5, enc.X10, 16)
		f.a.Constant32(enc.X5, typeIndex)
		f.a.Sd(enc.X5, enc.X10, 24)
		f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArraySet})
		f.restoreCallTransport()
	}
}

func (f *integerEmitter) gcDataArrayAllocation(base int, typeIndex, dataIndex uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0) // source byte offset
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // element count
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, dataIndex)
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocData, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) gcElementArrayAllocation(base int, typeIndex, elemIndex uint32, hiddenRefs []bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0) // source element offset
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // element count
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, elemIndex)
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocElem, safepoint: f.recordGCFrameSafepoint(hiddenRefs)})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) gcElementDrop(elemIndex uint32) {
	f.restoreCallTransport()
	f.a.Constant32(enc.X5, elemIndex)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayDropElem})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArrayInitializeElements(base int, typeIndex, elemIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // destination array reference
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // destination element offset
	f.Load(5, f.SpillOffset(base+2), false)
	f.a.Sd(enc.X5, enc.X10, 16) // source element offset
	f.Load(5, f.SpillOffset(base+3), false)
	f.a.Sd(enc.X5, enc.X10, 24) // element count
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 32)
	f.a.Constant32(enc.X5, elemIndex)
	f.a.Sd(enc.X5, enc.X10, 40)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayInitElem})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArrayInitializeData(base int, typeIndex, dataIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // destination array reference
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // destination element offset
	f.Load(5, f.SpillOffset(base+2), false)
	f.a.Sd(enc.X5, enc.X10, 16) // source byte offset
	f.Load(5, f.SpillOffset(base+3), false)
	f.a.Sd(enc.X5, enc.X10, 24) // element count
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 32)
	f.a.Constant32(enc.X5, dataIndex)
	f.a.Sd(enc.X5, enc.X10, 40)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayInitData})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArrayFillI32(base int, typeIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // destination array reference
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // destination element offset
	f.Load(5, f.SpillOffset(base+2), false)
	f.a.Sd(enc.X5, enc.X10, 16) // fill value
	f.Load(5, f.SpillOffset(base+3), false)
	f.a.Sd(enc.X5, enc.X10, 24) // element count
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 32)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayFill})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArrayCopyI32(base int, dstType, srcType uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // destination array reference
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // destination element offset
	f.Load(5, f.SpillOffset(base+2), true)
	f.a.Sd(enc.X5, enc.X10, 16) // source array reference
	f.Load(5, f.SpillOffset(base+3), false)
	f.a.Sd(enc.X5, enc.X10, 24) // source element offset
	f.Load(5, f.SpillOffset(base+4), false)
	f.a.Sd(enc.X5, enc.X10, 32) // element count
	f.a.Constant32(enc.X5, dstType)
	f.a.Sd(enc.X5, enc.X10, 40)
	f.a.Constant32(enc.X5, srcType)
	f.a.Sd(enc.X5, enc.X10, 48)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayCopy})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArrayGetI32(base int, typeIndex, helper uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: helper})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Lwu(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, false)
}

func (f *integerEmitter) gcArrayGetRef(base int, typeIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayGet})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) gcArraySetI32(base int, typeIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.Load(5, f.SpillOffset(base+2), false)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArraySet})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArraySetRef(base int, typeIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.Load(5, f.SpillOffset(base+2), true)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArraySet})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcArrayLength(base int) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayLen})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Lwu(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, false)
}

// gcStructGetField0 calls the exact numeric field getter used by the bounded
// struct.new_default/struct.get checkpoint.
func (f *integerEmitter) gcStructGetField0(base int) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // object reference
	f.a.Constant32(enc.X5, 0)
	f.a.Sd(enc.X5, enc.X10, 8)  // type index
	f.a.Sd(enc.X5, enc.X10, 16) // field index
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructGet})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Lwu(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, false)
}

func (f *integerEmitter) gcStructGetI32(base int, typeIndex, fieldIndex, helper uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // object reference
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 8) // type index
	f.a.Constant32(enc.X5, fieldIndex)
	f.a.Sd(enc.X5, enc.X10, 16) // field index
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: helper})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Lwu(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, false)
}

func (f *integerEmitter) gcStructSetI32(base int, typeIndex, fieldIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0) // object reference
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // field value
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, fieldIndex)
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructSet})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcObjectTableSet(base int, tableIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0) // zero-normalized table index
	f.Load(5, f.SpillOffset(base+1), true)
	f.a.Sd(enc.X5, enc.X10, 8) // compact GC reference
	f.a.Constant32(enc.X5, tableIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructTableSet})
	f.restoreCallTransport()
}

func (f *integerEmitter) gcObjectRefTest(base int, heap int64, nullable bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.a.Constant64(enc.X5, heap)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, 0)
	if nullable {
		f.a.Constant32(enc.X5, 1)
	}
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, 0) // exact heap markers are not admitted here.
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructRefTest})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Lwu(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, false)
}

// gcObjectRefTestBranch leaves the tested carrier in its spill slot and returns
// the zero-normalized predicate in X5 for an immediately following branch.
func (f *integerEmitter) gcObjectRefTestBranch(base int, heap int64, nullable bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.a.Constant64(enc.X5, heap)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, 0)
	if nullable {
		f.a.Constant32(enc.X5, 1)
	}
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, 0)
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructRefTest})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Lwu(enc.X5, result, 0)
}

func (f *integerEmitter) gcObjectRefCast(base int, heap int64, nullable bool) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.a.Constant64(enc.X5, heap)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, 0)
	if nullable {
		f.a.Constant32(enc.X5, 1)
	}
	f.a.Sd(enc.X5, enc.X10, 16)
	f.a.Constant32(enc.X5, 0) // exact heap markers are not admitted here.
	f.a.Sd(enc.X5, enc.X10, 24)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcStructRefCast})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) gcObjectConversion(base int, helper uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), true)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: helper})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) abstractReferenceTest(base int, heap int64, nullable bool) {
	f.Load(5, f.SpillOffset(base), true)
	switch heap {
	case -13, -14: // Nullable bottom types contain only null; non-null bottom is empty.
		if nullable {
			f.a.Sltiu(enc.X5, enc.X5, 1)
		} else {
			f.a.Constant32(enc.X5, 0)
		}
	case -16, -17: // func/extern contain every value in their source hierarchy.
		if nullable {
			f.a.Constant32(enc.X5, 1)
		} else {
			f.a.Sltu(enc.X5, enc.Zero, enc.X5)
		}
	}
	f.Store(f.SpillOffset(base), 5, false)
}

// Resolve the current activation's dynamic host target before any call-arena
// write. Equal linear memory alone does not establish instance identity.
func (f *integerEmitter) importCallTarget(index uint32) {
	f.a.Ld(enc.X6, enc.X25, -abi.ImportDispatchPtrOffset)
	f.trap(trapImportMissing, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Constant64(enc.X7, int64(index)*coreruntime.ImportDispatchEntryBytes)
	f.a.Add(enc.X6, enc.X6, enc.X7)
	f.a.Ld(enc.X28, enc.X6, coreruntime.ImportDispatchCodePtrOffset)
	f.trap(trapImportMissing, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Constant32(enc.X7, 3)
	f.a.And(enc.X7, enc.X28, enc.X7)
	f.trap(trapImportMissing, f.a.BranchLong(enc.NE, enc.X7, enc.Zero))
	f.a.Ld(enc.X7, enc.X6, coreruntime.ImportDispatchHomeLinMemOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.NE, enc.X7, enc.X25))
	f.a.Ld(enc.X7, enc.X6, coreruntime.ImportDispatchTargetContextOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X5, enc.X6, coreruntime.ImportDispatchCallerContextOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	f.a.Ld(enc.X6, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Ld(enc.X6, enc.X6, coreruntime.FuncRefContextOffset)
	f.trap(trapImportContext, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.trap(trapImportContext, f.a.BranchLong(enc.NE, enc.X7, enc.X6))
}
