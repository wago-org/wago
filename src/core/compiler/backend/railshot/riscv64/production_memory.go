//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

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

func (f *integerEmitter) productionMemorySize(out enc.Reg) {
	f.a.Ld(out, enc.X25, -abi.ActualLinMemByteSize64Offset)
}

// The whole module and runtime descriptors admit one nonshared memory32. The
// stable reservation is already mapped; growth only publishes size caches.
// Optional directory entry 0 supplies the instance quota, as on other targets.
func (f *integerEmitter) productionMemorySizeGrow(grow bool) {
	f.a.Lwu(enc.X6, enc.X25, -abi.LinMemCurrentPagesOffset)
	if !grow {
		f.Move(5, 6, false)
		return
	}
	zero := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	// Use 64-bit addition: even UINT32_MAX delta plus current pages cannot
	// overflow this carrier, and exceeds the at-most-65536-page ceiling.
	f.a.Add(enc.X7, enc.X5, enc.X6)
	f.a.Lwu(enc.X28, enc.X25, -abi.LinMemMaxPagesOffset)
	failMax := f.a.BranchLong(enc.LTU, enc.X28, enc.X7)
	f.a.Ld(enc.X28, enc.X25, -abi.MemoryDirPtrOffset)
	noDir := f.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	f.a.Lwu(enc.X28, enc.X28, abi.MemoryDirPolicyMaxPagesOffset)
	noPolicy := f.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	failPolicy := f.a.BranchLong(enc.LTU, enc.X28, enc.X7)
	f.patchBulk(noDir, noPolicy)
	f.a.Sw(enc.X7, enc.X25, -abi.LinMemCurrentPagesOffset)
	f.a.Slli(enc.X7, enc.X7, 16)
	f.a.Sd(enc.X7, enc.X25, -abi.ActualLinMemByteSize64Offset)
	f.a.Sw(enc.X7, enc.X25, -abi.LinMemLegacyBytesOffset)
	f.patchBulk(zero)
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
