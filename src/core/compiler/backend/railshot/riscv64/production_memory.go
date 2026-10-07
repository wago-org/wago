//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

// Every indexed memory is ordinary wasm32 and uses the shared bounded ceiling.
func admitMemory32(t wasm.MemType, production bool) error {
	maxPages := uint64(native.MaxMemoryPages)
	if production {
		maxPages = 65536
	}
	if t.Shared || t.Limits.Addr64 || t.Limits.Min > maxPages || t.Limits.HasMax && (t.Limits.Max < t.Limits.Min || t.Limits.Max > 65536) {
		return unsupported("shared/memory64/initial memory bound")
	}
	return nil
}

// Globals are a pointer directory of aliased cells, never inline values. I32
// writes replace the complete numeric cell with a zero-normalized carrier.
func (f *integerEmitter) productionGlobal(index uint32, wide, store bool) {
	f.a.Ld(enc.X28, enc.X25, -abi.GlobalsPtrOffset)
	offset := int64(index) * 8
	if offset > 2047 {
		f.a.Constant64(enc.Scratch1, offset)
		f.a.Add(enc.X28, enc.X28, enc.Scratch1)
		offset = 0
	}
	f.a.Ld(enc.X28, enc.X28, int32(offset))
	if store {
		if !wide {
			f.NormalizeI32(5)
		}
		f.a.Sd(enc.X5, enc.X28, 0)
	} else if wide {
		f.a.Ld(enc.X5, enc.X28, 0)
	} else {
		f.a.Lwu(enc.X5, enc.X28, 0)
	}
}

// A directory row names the actual JobMemory, so aliased indices observe growth
// immediately through authoritative basedata rather than stale directory caches.
// X25 remains the execution home. Index zero also works without a directory.
func (f *integerEmitter) productionMemoryBaseAt(index uint32, out enc.Reg) {
	if index == 0 {
		f.a.Addi(out, enc.X25, 0)
		return
	}
	f.productionMemoryDirectoryAt(index, out)
	f.a.Ld(out, out, abi.MemoryDirBaseOffset)
}

func (f *integerEmitter) productionMemoryDirectoryAt(index uint32, out enc.Reg) {
	f.a.Ld(out, enc.X25, -abi.MemoryDirPtrOffset)
	offset := int64(index) * abi.MemoryDirEntryBytes
	if offset <= 2047 {
		f.a.Addi(out, out, int32(offset))
	} else {
		f.a.Constant64(enc.Scratch1, offset)
		f.a.Add(out, out, enc.Scratch1)
	}
}

func (f *integerEmitter) productionMemorySizeAt(index uint32, out enc.Reg) {
	f.productionMemoryBaseAt(index, out)
	f.a.Ld(out, out, -abi.ActualLinMemByteSize64Offset)
}

// Stable reservations are already mapped. All guards precede cache publication;
// a zero delta bypasses quotas, matching the existing targets. Reload addresses
// after long branches, whose veneers consume Scratch1.
func (f *integerEmitter) productionMemorySizeGrowAt(index uint32, grow bool) {
	f.productionMemoryBaseAt(index, enc.X28)
	f.a.Lwu(enc.X6, enc.X28, -abi.LinMemCurrentPagesOffset)
	if !grow {
		f.Move(5, 6, false)
		return
	}
	zero := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	f.a.Add(enc.X7, enc.X5, enc.X6)
	f.a.Lwu(enc.X29, enc.X28, -abi.LinMemMaxPagesOffset)
	failMax := f.a.BranchLong(enc.LTU, enc.X29, enc.X7)
	f.a.Ld(enc.X28, enc.X25, -abi.MemoryDirPtrOffset)
	noDir := f.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	f.productionMemoryDirectoryAt(index, enc.X28)
	f.a.Lwu(enc.X29, enc.X28, abi.MemoryDirPolicyMaxPagesOffset)
	noPolicy := f.a.BranchLong(enc.EQ, enc.X29, enc.Zero)
	failPolicy := f.a.BranchLong(enc.LTU, enc.X29, enc.X7)
	f.patchBulk(noDir, noPolicy)
	f.productionMemoryBaseAt(index, enc.X28)
	f.a.Sw(enc.X7, enc.X28, -abi.LinMemCurrentPagesOffset)
	f.a.Slli(enc.X29, enc.X7, 16)
	f.a.Sd(enc.X29, enc.X28, -abi.ActualLinMemByteSize64Offset)
	f.a.Sw(enc.X29, enc.X28, -abi.LinMemLegacyBytesOffset)
	// Update the selected row too; other aliases read their shared basedata.
	f.a.Ld(enc.X28, enc.X25, -abi.MemoryDirPtrOffset)
	noPublishDir := f.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	f.productionMemoryDirectoryAt(index, enc.X28)
	f.a.Sd(enc.X29, enc.X28, abi.MemoryDirCurrentBytesOffset)
	f.a.Sw(enc.X7, enc.X28, abi.MemoryDirCurrentPagesOffset)
	f.patchBulk(noPublishDir, zero)
	f.Move(5, 6, false)
	end := f.Jump()
	f.patchBulk(failMax, failPolicy)
	f.Constant(5, -1, false)
	f.patchBulk(end)
}

// Passive descriptors are {ptr u64, len u32, pad u32}. Only the length is
// mutable; data.drop must preserve neighboring descriptors and the pad word.
func (f *integerEmitter) productionDataAddress(offset int32, out enc.Reg) {
	f.a.Ld(out, enc.X25, -abi.PassiveDataPtrOffset)
	if offset <= 2047 {
		f.a.Addi(out, out, offset)
	} else {
		f.a.Constant64(enc.Scratch1, int64(offset))
		f.a.Add(out, out, enc.Scratch1)
	}
}
