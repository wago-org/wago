//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// A public invocation view is either the Engine's exact reserved top or a full
// activation-owned view above the current frame and below the root bridge.
// Bound the entire span before reading it. This is an explicit retained-context
// contract, never a native-pointer owner lookup or a guest memory address.
// X29/X30/X31 are clobbered; the view cursor must be a different register.
func nativeInvocationViewGuards(a *enc.Asm, view enc.Reg, aboveSP int) []int {
	return nativeInvocationViewGuardsInto(a, view, aboveSP, nil)
}

func nativeInvocationViewGuardsInto(a *enc.Asm, view enc.Reg, aboveSP int, bad []int) []int {
	guard := func(c enc.Cond, l, r enc.Reg) { bad = append(bad, a.BranchLong(c, l, r)) }
	a.Addi(enc.Scratch0, enc.Zero, 7)
	a.And(enc.Scratch0, view, enc.Scratch0)
	guard(enc.NE, enc.Scratch0, enc.Zero)
	a.Ld(enc.X29, enc.X25, -abi.RV64TrapReentrySPOffset)
	a.Addi(enc.Scratch1, enc.X29, abi.RV64NativeWrapperSaveBytes)
	guard(enc.LTU, enc.Scratch1, enc.X29)
	a.Addi(enc.Scratch0, enc.Zero, 15)
	a.And(enc.Scratch0, enc.Scratch1, enc.Scratch0)
	guard(enc.NE, enc.Scratch0, enc.Zero)
	root := a.BranchLong(enc.EQ, view, enc.Scratch1)
	a.Constant64(enc.Scratch0, int64(aboveSP))
	a.Add(enc.Scratch0, enc.SP, enc.Scratch0)
	guard(enc.LTU, enc.Scratch0, enc.SP)
	guard(enc.LTU, view, enc.Scratch0)
	guard(enc.LTU, enc.X29, view)
	a.Sub(enc.Scratch0, enc.X29, view)
	a.Addi(enc.X29, enc.Zero, abi.RV64InvocationViewBytes)
	guard(enc.LTU, enc.Scratch0, enc.X29)
	if err := a.PatchLong(root, int64(a.Len())); err != nil {
		panic(err)
	}
	// Taken long veneers clobber X31; reconstruct the authoritative root top.
	a.Ld(enc.X29, enc.X25, -abi.RV64TrapReentrySPOffset)
	a.Addi(enc.Scratch1, enc.X29, abi.RV64NativeWrapperSaveBytes)
	a.Ld(enc.X29, view, abi.RV64InvocationTopOffset)
	guard(enc.NE, enc.X29, enc.Scratch1)
	a.Ld(enc.X29, view, 0)
	a.Constant64(enc.Scratch0, int64(abi.RV64InvocationMagic))
	guard(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, view, abi.RV64InvocationFenceOffset)
	a.Ld(enc.Scratch0, enc.X25, -abi.StackFenceOffset)
	guard(enc.NE, enc.X29, enc.Scratch0)
	return bad
}

func (f *target) initializeNativeInvocation(frame int, info FunctionInfo) []int {
	if !f.mode.invocationContext {
		return nil
	}
	a := &f.a
	a.Ld(enc.X9, enc.X25, -abi.RV64InvocationViewOffset)
	bad := nativeInvocationViewGuards(a, enc.X9, frame)
	// A narrow entry may call a wider local function using this same view.
	// Check the module's complete shared transport before any call can stage
	// beyond a retained foreign activation's buffers.
	for _, direction := range []struct {
		capacity int32
		slots    uint16
	}{
		{abi.RV64InvocationArgumentBytesOffset, f.mode.moduleArgumentSlots},
		{abi.RV64InvocationResultBytesOffset, f.mode.moduleResultSlots},
	} {
		if direction.slots > 256 {
			a.Ld(enc.X29, enc.X9, direction.capacity)
			a.Constant64(enc.Scratch0, int64(direction.slots)*8)
			bad = append(bad, a.BranchLong(enc.LTU, enc.X29, enc.Scratch0))
		}
	}
	a.Ld(enc.X29, enc.X9, abi.RV64InvocationFirstRAOffset)
	initialized := a.BranchLong(enc.NE, enc.X29, enc.Zero)
	guard := func(c enc.Cond, l, r enc.Reg) { bad = append(bad, a.BranchLong(c, l, r)) }
	for _, direction := range []struct {
		pointer, capacity int32
		incoming          enc.Reg
		cells             int
	}{{abi.RV64InvocationArgumentsOffset, abi.RV64InvocationArgumentBytesOffset, enc.X10, info.Params}, {abi.RV64InvocationResultsOffset, abi.RV64InvocationResultBytesOffset, enc.X13, info.Results}} {
		a.Ld(enc.X29, enc.X9, direction.pointer)
		guard(enc.NE, enc.X29, direction.incoming)
		a.Ld(enc.X29, enc.X9, direction.capacity)
		a.Constant64(enc.Scratch0, int64(8*direction.cells))
		guard(enc.LTU, enc.X29, enc.Scratch0)
	}
	a.Ld(enc.X29, enc.SP, 0)
	a.Sd(enc.X29, enc.X9, abi.RV64InvocationFirstRAOffset)
	if err := a.PatchLong(initialized, int64(a.Len())); err != nil {
		panic(err)
	}
	return bad
}

// The caller has already authenticated the parent view and checked this full
// activation's descent. The new view owns only its actual transport; all storage
// and code lifetime obligations stay with the root execution lease.
func nativeInvocationActivation(a *enc.Asm, parent enc.Reg, viewOffset, argsOffset, resultsOffset, transportBytes int) {
	tailRAAddress(a, enc.X9, enc.SP, viewOffset)
	a.Constant64(enc.Scratch0, int64(abi.RV64InvocationMagic))
	a.Sd(enc.Scratch0, enc.X9, 0)
	for _, off := range []int32{abi.RV64InvocationFenceOffset, abi.RV64InvocationTopOffset} {
		a.Ld(enc.Scratch0, parent, off)
		a.Sd(enc.Scratch0, enc.X9, off)
	}
	tailRAAddress(a, enc.Scratch0, enc.SP, argsOffset)
	a.Sd(enc.Scratch0, enc.X9, abi.RV64InvocationArgumentsOffset)
	tailRAAddress(a, enc.Scratch0, enc.SP, resultsOffset)
	a.Sd(enc.Scratch0, enc.X9, abi.RV64InvocationResultsOffset)
	a.Constant64(enc.Scratch0, int64(transportBytes))
	a.Sd(enc.Scratch0, enc.X9, abi.RV64InvocationArgumentBytesOffset)
	a.Sd(enc.Scratch0, enc.X9, abi.RV64InvocationResultBytesOffset)
	a.Sd(enc.Zero, enc.X9, abi.RV64InvocationFirstRAOffset)
}
