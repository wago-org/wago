//go:build amd64

// Package hostthunk emits the small per-instance native adapters needed when a
// host function is reached through a Wasm table. It is runtime-owned so loading
// precompiled artifacts does not retain the Railshot compiler.
package hostthunk

import "github.com/wago-org/wago/src/core/encoder/amd64"

const (
	offCustomCtx = 40
	hcTrampoline = 56
	hcImportIdx  = 64
	hcNArgs      = 68
	hcArgs       = 72
	hcResults    = 584
)

func Indirect(importIdx uint32) []byte {
	a := &amd64.Asm{}
	a.Load32(amd64.RAX, amd64.RDI, 0)
	a.Load64(amd64.R8, amd64.RSI, -offCustomCtx)
	a.Load32(amd64.RCX, amd64.R8, 0)
	a.LeaScaled(amd64.RDX, amd64.R8, amd64.RCX, 3, 8)
	a.StoreImm32Mem(amd64.RDX, 0, int32(importIdx))
	a.Store32(amd64.RDX, 4, amd64.RAX)
	a.AluRI(0, amd64.RCX, 1, false)
	a.Store32(amd64.R8, 0, amd64.RCX)
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
	for i := 0; i < paramSlots; i++ {
		a.Load64(amd64.RAX, amd64.RDI, int32(i*8))
		a.Store64(amd64.R8, hcArgs+int32(i*8), amd64.RAX)
	}
	a.StoreImm32Mem(amd64.R8, hcImportIdx, int32(importIdx))
	a.StoreImm32Mem(amd64.R8, hcNArgs, int32(paramSlots|resultSlots<<16))
	a.CallMem(amd64.R8, hcTrampoline)
	a.Load64(amd64.R8, amd64.RBX, -offCustomCtx)
	a.Pop(amd64.RCX)
	for i := 0; i < resultSlots; i++ {
		a.Load64(amd64.RAX, amd64.R8, hcResults+int32(i*8))
		a.Store64(amd64.RCX, int32(i*8), amd64.RAX)
	}
	if resultSlots > 0 {
		a.Load64(amd64.RAX, amd64.R8, hcResults)
	}
	if resultSlots > 1 {
		a.Load64(amd64.RDX, amd64.R8, hcResults+8)
	}
	a.Pop(amd64.RBX)
	a.Ret()
	return a.B
}
