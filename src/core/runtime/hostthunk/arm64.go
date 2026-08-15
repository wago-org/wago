//go:build arm64

package hostthunk

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

const (
	offCustomCtx = 40
	hcTrampoline = 176
	hcImportIdx  = 184
	hcNArgs      = 188
	hcArgs       = 192
	hcResults    = 704
)

func Indirect(importIdx uint32) []byte {
	a := &a64.Asm{DisableLogicalMoveImmediate: true}
	a.Load32(a64.X9, a64.X0, 0)
	a.SubImm64(a64.X10, a64.X1, offCustomCtx)
	a.Load64(a64.X10, a64.X10, 0)
	a.Load32(a64.X11, a64.X10, 0)
	a.AddShifted(a64.X12, a64.X10, a64.X11, 3, false)
	a.AddImm64(a64.X12, a64.X12, 8)
	a.MovImm64(a64.X16, uint64(importIdx))
	a.Store32(a64.X16, a64.X12, 0)
	a.Store32(a64.X9, a64.X12, 4)
	a.AddImm32(a64.X11, a64.X11, 1)
	a.Store32(a64.X11, a64.X10, 0)
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
	a := &a64.Asm{DisableLogicalMoveImmediate: true}
	const linMem = a64.X26
	a.StpPre(linMem, a64.X3, a64.SP, -32)
	a.Store64(a64.LR, a64.SP, 16)
	if useHome {
		a.MovReg64(linMem, a64.X1)
	}
	a.SubImm64(a64.X10, linMem, offCustomCtx)
	a.Load64(a64.X10, a64.X10, 0)
	for i := 0; i < paramSlots; i++ {
		a.Load64(a64.X9, a64.X0, uint32(i*8))
		a.Store64(a64.X9, a64.X10, uint32(hcArgs+i*8))
	}
	a.MovImm64(a64.X16, uint64(importIdx))
	a.Store32(a64.X16, a64.X10, hcImportIdx)
	a.MovImm64(a64.X16, uint64(uint32(paramSlots)|uint32(resultSlots)<<16))
	a.Store32(a64.X16, a64.X10, hcNArgs)
	a.Load64(a64.X16, a64.X10, hcTrampoline)
	a.Blr(a64.X16)
	a.SubImm64(a64.X10, linMem, offCustomCtx)
	a.Load64(a64.X10, a64.X10, 0)
	a.Load64(a64.X3, a64.SP, 8)
	for i := 0; i < resultSlots; i++ {
		a.Load64(a64.X9, a64.X10, uint32(hcResults+i*8))
		a.Store64(a64.X9, a64.X3, uint32(i*8))
	}
	a.Load64(a64.LR, a64.SP, 16)
	a.LdpPost(linMem, a64.X3, a64.SP, 32)
	a.Ret()
	return a.B
}
