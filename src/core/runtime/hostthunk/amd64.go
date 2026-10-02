//go:build amd64

// Package hostthunk emits the small per-instance native adapters needed when a
// host function is reached through a Wasm table. It is runtime-owned so loading
// precompiled artifacts does not retain the Railshot compiler.
package hostthunk

import "github.com/wago-org/wago/src/core/encoder/amd64"

const (
	offTrapStackReentry = 24
	offCustomCtx        = 40
	offTrapCellPtr      = 104
	hostCallLogEntries  = 1 << 13
	trapHostEventFull   = 22
	hcTrampoline        = 56
	hcImportIdx         = 64
	hcNArgs             = 68
	hcArgs              = 72
	hcResults           = 584
)

const (
	hcWideBase             = hcResults + maxSyncHostSlots*8
	hcWideArgs       int32 = 8
	maxSyncHostSlots       = 64
)

func Indirect(importIdx uint32) []byte {
	a := &amd64.Asm{}
	a.Load32(amd64.RAX, amd64.RDI, 0)
	a.Load64(amd64.R8, amd64.RSI, -offCustomCtx)
	a.Load32(amd64.RCX, amd64.R8, 0)
	a.AluRI(7, amd64.RCX, hostCallLogEntries, false)
	full := a.JccPlaceholder(amd64.CondAE)
	a.LeaScaled(amd64.RDX, amd64.R8, amd64.RCX, 3, 8)
	a.StoreImm32Mem(amd64.RDX, 0, int32(importIdx))
	a.Store32(amd64.RDX, 4, amd64.RAX)
	a.AluRI(0, amd64.RCX, 1, false)
	a.Store32(amd64.R8, 0, amd64.RCX)
	a.Ret()
	a.PatchRel32(full, a.Len())
	a.Load64(amd64.R8, amd64.RSI, -offTrapCellPtr)
	a.StoreImm32Mem(amd64.R8, 0, trapHostEventFull)
	a.Load64(amd64.RSP, amd64.RSI, -offTrapStackReentry)
	a.Ret()
	return a.B
}

func IndirectSync(importIdx uint32, paramSlots, resultSlots int) []byte {
	return indirectSync(importIdx, paramSlots, resultSlots, true)
}

func IndirectOwnedSync(importIdx uint32, paramSlots, resultSlots int) []byte {
	return indirectSync(importIdx, paramSlots, resultSlots, false)
}

func indirectSync(importIdx uint32, paramSlots, resultSlots int, useHome bool) []byte {
	a := &amd64.Asm{}
	a.Push(amd64.RBX)
	a.Push(amd64.RCX)
	if useHome {
		a.MovReg64(amd64.RBX, amd64.RSI)
	}
	a.Load64(amd64.R8, amd64.RBX, -offCustomCtx)
	// A signature wider in either direction uses the appended exchange areas
	// for both directions, matching the host loop's all-or-nothing selection.
	wide := paramSlots > maxSyncHostSlots || resultSlots > maxSyncHostSlots
	argBase := amd64.R8
	argOffset := int32(hcArgs)
	if wide {
		argBase = amd64.R9
		argOffset = 0
		a.MovReg64(amd64.R9, amd64.R8)
		a.LeaDisp(amd64.R9, amd64.R9, hcWideBase+hcWideArgs)
	}
	for i := 0; i < paramSlots; i++ {
		a.Load64(amd64.RAX, amd64.RDI, int32(i*8))
		a.Store64(argBase, argOffset+int32(i*8), amd64.RAX)
	}
	a.StoreImm32Mem(amd64.R8, hcImportIdx, int32(importIdx))
	a.StoreImm32Mem(amd64.R8, hcNArgs, int32(paramSlots|resultSlots<<16))
	a.CallMem(amd64.R8, hcTrampoline)
	a.Load64(amd64.R8, amd64.RBX, -offCustomCtx)
	a.Pop(amd64.RCX)
	resultBase := amd64.R8
	resultOffset := int32(hcResults)
	if wide {
		resultBase = amd64.R9
		resultOffset = 0
		a.Load32(amd64.R9, amd64.R8, hcWideBase+4)
		a.LeaScaled(amd64.R9, amd64.R8, amd64.R9, 3, hcWideBase+hcWideArgs)
	}
	for i := 0; i < resultSlots; i++ {
		a.Load64(amd64.RAX, resultBase, resultOffset+int32(i*8))
		a.Store64(amd64.RCX, int32(i*8), amd64.RAX)
	}
	if resultSlots > 0 {
		a.Load64(amd64.RAX, resultBase, resultOffset)
	}
	if resultSlots > 1 {
		a.Load64(amd64.RDX, resultBase, resultOffset+8)
	}
	a.Pop(amd64.RBX)
	a.Ret()
	return a.B
}
