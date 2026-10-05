//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// These RV64 wire offsets describe the qualified runtime prefix. Runtime tests
// compare them to the mechanically derived native frame before executing thunks.
const (
	HostSavedPrefixBytes  = 208
	HostTrampolineOffset  = HostSavedPrefixBytes
	HostImportIndexOffset = HostTrampolineOffset + 8
	HostSlotCountsOffset  = HostImportIndexOffset + 4
	HostArgsOffset        = HostSlotCountsOffset + 4
	HostResultsOffset     = HostArgsOffset + 64*8
	HostFixedFrameBytes   = HostResultsOffset + 64*8
	hostWideHeaderBytes   = 8
)

// HostIndirectSyncThunk emits a genuine RV64 wrapper entry. Its scalar slots
// already carry the typed caller's normalized i32/f32 or raw i64/f64 bits. The
// wrapper uses the passed home; an owned thunk keeps the activation's pinned
// home instead. Both return through the qualified synchronous host protocol.
func HostIndirectSyncThunk(importIndex uint32, paramSlots, resultSlots int) []byte {
	return hostIndirectSyncThunk(importIndex, paramSlots, resultSlots, true)
}
func HostIndirectOwnedSyncThunk(importIndex uint32, paramSlots, resultSlots int) []byte {
	return hostIndirectSyncThunk(importIndex, paramSlots, resultSlots, false)
}

func hostIndirectSyncThunk(importIndex uint32, paramSlots, resultSlots int, useHome bool) []byte {
	if paramSlots < 0 || resultSlots < 0 || paramSlots > 65535 || resultSlots > 65535 {
		return nil
	}
	var a enc.Asm
	home := enc.X25
	if useHome {
		home = enc.X11
	}
	// Check the complete thunk frame before changing SP or writing any slot.
	a.Ld(enc.X29, home, -abi.StackFenceOffset)
	below := a.BranchLong(enc.LTU, enc.SP, enc.X29)
	a.Sub(enc.X28, enc.SP, enc.X29)
	a.Constant64(enc.X30, callFrameHeader)
	short := a.BranchLong(enc.LTU, enc.X28, enc.X30)
	a.Addi(enc.SP, enc.SP, -callFrameHeader)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X13, enc.SP, 8)
	a.Sd(enc.X10, enc.SP, 16)
	a.Sd(enc.X12, enc.SP, 24)
	a.Sd(enc.X25, enc.SP, 32)
	if useHome {
		a.Addi(enc.X25, enc.X11, 0)
	}
	a.Ld(enc.X28, enc.X25, -40)
	missingCtrl := a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	a.Ld(enc.X31, enc.X28, HostTrampolineOffset)
	missingTrampoline := a.BranchLong(enc.EQ, enc.X31, enc.Zero)
	wide := paramSlots > 64 || resultSlots > 64
	var invalidWide []int
	if wide {
		// Validate the declared immutable exchange capacity before touching either
		// area; the allocation/length owner remains the checked runtime caller.
		a.Lwu(enc.X5, enc.X28, HostFixedFrameBytes)
		a.Constant32(enc.X6, 0x5741474f)
		invalidWide = append(invalidWide, a.BranchLong(enc.NE, enc.X5, enc.X6))
		a.Lwu(enc.X5, enc.X28, HostFixedFrameBytes+4)
		a.Constant32(enc.X6, uint32(max(paramSlots, resultSlots)))
		invalidWide = append(invalidWide, a.BranchLong(enc.LTU, enc.X5, enc.X6))
		a.Constant32(enc.X6, 65535)
		invalidWide = append(invalidWide, a.BranchLong(enc.LTU, enc.X6, enc.X5))
	}
	argOffset := HostArgsOffset
	if wide {
		argOffset = HostFixedFrameBytes + hostWideHeaderBytes
	}
	a.Addi(enc.X6, enc.X28, int32(argOffset))
	a.Addi(enc.X7, enc.X10, 0)
	// Advancing cursors keep each LD/SD fixed at offset0, including slots beyond
	// the signed12-bit displacement window. No optional ISA extension is used.
	for range paramSlots {
		a.Ld(enc.X5, enc.X7, 0)
		a.Sd(enc.X5, enc.X6, 0)
		a.Addi(enc.X7, enc.X7, 8)
		a.Addi(enc.X6, enc.X6, 8)
	}
	a.Constant32(enc.X5, importIndex)
	a.Sw(enc.X5, enc.X28, HostImportIndexOffset)
	a.Constant32(enc.X5, uint32(paramSlots)|uint32(resultSlots)<<16)
	a.Sw(enc.X5, enc.X28, HostSlotCountsOffset)
	a.Jalr(enc.RA, enc.X31, 0)
	a.Ld(enc.X28, enc.X25, -40)
	if wide {
		a.Lwu(enc.X5, enc.X28, HostFixedFrameBytes+4)
		a.Slli(enc.X5, enc.X5, 3)
		a.Add(enc.X6, enc.X28, enc.X5)
		a.Addi(enc.X6, enc.X6, HostFixedFrameBytes+hostWideHeaderBytes)
	} else {
		a.Addi(enc.X6, enc.X28, HostResultsOffset)
	}
	a.Ld(enc.X7, enc.SP, 8)
	for range resultSlots {
		a.Ld(enc.X5, enc.X6, 0)
		a.Sd(enc.X5, enc.X7, 0)
		a.Addi(enc.X6, enc.X6, 8)
		a.Addi(enc.X7, enc.X7, 8)
	}
	a.Ld(enc.RA, enc.SP, 0)
	a.Ld(enc.X25, enc.SP, 32)
	a.Addi(enc.SP, enc.SP, callFrameHeader)
	a.Ret()
	// A missing protocol returns through the original owning activation; it never
	// calls Go directly or leaves a callable alternate-target implementation.
	missing := len(a.B)
	for _, site := range append(invalidWide, missingCtrl, missingTrampoline) {
		if err := a.PatchLong(site, int64(missing)); err != nil {
			return nil
		}
	}
	a.Constant32(enc.X5, abi.TrapCalledFnNotLinked)
	a.Ld(enc.X12, enc.X25, -abi.TrapCellPtrOffset)
	a.Sw(enc.X5, enc.X12, 0)
	a.Ld(enc.SP, enc.X25, -abi.RV64TrapReentrySPOffset)
	a.Ld(enc.X31, enc.X25, -abi.RV64TrapContinuationOffset)
	a.Jalr(enc.Zero, enc.X31, 0)
	bounds := len(a.B)
	if err := a.PatchLong(below, int64(bounds)); err != nil {
		return nil
	}
	if err := a.PatchLong(short, int64(bounds)); err != nil {
		return nil
	}
	a.Ld(enc.X12, home, -abi.TrapCellPtrOffset)
	a.Constant32(enc.X5, abi.TrapStackFenceBreached)
	a.Sw(enc.X5, enc.X12, 0)
	a.Ld(enc.SP, home, -abi.RV64TrapReentrySPOffset)
	a.Ld(enc.X31, home, -abi.RV64TrapContinuationOffset)
	a.Jalr(enc.Zero, enc.X31, 0)
	if a.Err() != nil {
		return nil
	}
	return a.B
}
