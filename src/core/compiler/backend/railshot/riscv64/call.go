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
}

const (
	gcStructDispatchBit  uint32 = 1 << 30
	gcStructAllocDefault uint32 = 1
	gcStructGet          uint32 = 2
	gcArrayAllocDefault  uint32 = 16
	gcArrayGet           uint32 = 17
	gcArraySet           uint32 = 20
	gcArrayLen           uint32 = 21
	gcArrayAllocFixed    uint32 = 22
	gcArrayAllocUniform  uint32 = 23
)

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
func (f *integerEmitter) gcDefaultAllocation(helper uint32, base int, array bool, typeIndex uint32) {
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
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: helper})
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
func (f *integerEmitter) gcUniformArrayAllocation(base int, typeIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0) // uniform value
	f.Load(5, f.SpillOffset(base+1), false)
	f.a.Sd(enc.X5, enc.X10, 8) // length
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16) // type index
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocUniform})
	f.restoreCallTransport()
	result := enc.X11
	if f.mode.production {
		result = enc.X13
	}
	f.a.Ld(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, true)
}

func (f *integerEmitter) gcFixedArrayAllocation(base int, typeIndex, count uint32) {
	f.restoreCallTransport()
	for i := uint32(0); i < count; i++ {
		f.Load(5, f.SpillOffset(base+int(i)), false)
		f.a.Sd(enc.X5, enc.X10, int32(i*8))
	}
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, int32(count*8))
	f.a.Constant32(enc.X5, count)
	f.a.Sd(enc.X5, enc.X10, int32((count+1)*8))
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocFixed, helperSlots: int(count) + 2})
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
func (f *integerEmitter) gcFixedArrayAllocation3ViaUniform(base int, typeIndex uint32) {
	f.restoreCallTransport()
	f.Load(5, f.SpillOffset(base), false)
	f.a.Sd(enc.X5, enc.X10, 0)
	f.a.Constant32(enc.X5, 3)
	f.a.Sd(enc.X5, enc.X10, 8)
	f.a.Constant32(enc.X5, typeIndex)
	f.a.Sd(enc.X5, enc.X10, 16)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcArrayAllocUniform})
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

func (f *integerEmitter) gcArrayGetI32(base int, typeIndex uint32) {
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
	f.a.Lwu(enc.X5, result, 0)
	f.Store(f.SpillOffset(base), 5, false)
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
