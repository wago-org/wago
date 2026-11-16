//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Only attestation policy 2 admits this lowering. Validated typed operands and
// checked public ingress establish parent/child identity and layout. Semantic
// null/bounds traps and the complete remembered-parent barrier remain necessary.
func (f *fn) emitTrustedDirectOffsetRefArraySet(t uint32, argSlot int) {
	if _, ok := f.directOffsetRefArrayChild(t); !ok {
		panic("amd64: invalid trusted reference store shape")
	}
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		r := f.locals[local].reg
		if r >= R9 && r <= R11 {
			preserve[r-R9] = true
		}
	}
	for _, global := range f.globalPinIndices[:f.nGlobalPins] {
		r := globalRegValue(f.globalReg[global])
		if r >= R9 && r <= R11 {
			preserve[r-R9] = true
		}
	}
	a.Push(R8)
	delta := int32(8)
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
			delta += 8
		}
	}
	var failure []int
	guard := func(c Cond) { failure = append(failure, a.JccPlaceholder(c)) }
	a.Load32(R10, RSP, f.spillOff(argSlot)+delta)
	a.TestSelf(R10, false)
	guard(condE)
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.DirectInstanceCollectorOffset)
	a.Load64(R9, R8, gc.DirectViewArenaOffset)
	a.Add64(R9, R10)
	a.Load32(RAX, RSP, f.spillOff(argSlot+1)+delta)
	a.AluRM(cmpRMcode, RAX, R9, 8, false)
	guard(condAE)
	// Index arithmetic stays 64-bit even though Wasm's logical index is i32.
	a.ShiftImm(4, RAX, 2, true)
	a.LeaDisp(RDI, R9, 16)
	a.Add64(RDI, RAX)
	a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryStartOffset, false)
	old := a.JccPlaceholder(condB)
	a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryBumpOffset, false)
	young := a.JccPlaceholder(condB)
	a.PatchRel32(old, a.Len())
	a.MovRegReg32(RAX, R10)
	a.ShiftImm(5, RAX, 3, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.DirectViewDirtyCountOffset, false)
	guard(condAE)
	a.Load64(RSI, R8, gc.DirectViewDirtyOffset)
	a.TestSelf(RSI, true)
	guard(condE)
	a.Add64(RSI, RAX)
	a.LoadIdx(RAX, RSI, RSP, 0, 1, false, false)
	a.AluRI(cmpDigit, RAX, 1, false)
	guard(condNE)
	a.PatchRel32(young, a.Len())
	// Null is an ordinary allowed child. No dynamic child check or raw address
	// survives: just copy the validated compact reference after every guard.
	a.Load32(RAX, RSP, f.spillOff(argSlot+2)+delta)
	a.Store32(RDI, 0, RAX)
	a.MovImm32(RAX, 1)
	done := a.JmpPlaceholder()
	for _, site := range failure {
		a.PatchRel32(site, a.Len())
	}
	a.MovImm32(RAX, 0)
	a.PatchRel32(done, a.Len())
	a.TestSelf(RAX, false)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Pop(R8)
}
