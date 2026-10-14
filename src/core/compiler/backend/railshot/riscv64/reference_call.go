//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// References are full-width canonical carriers supplied by a retained owner.
// Resolve null, identity and the exact declared signature before marshalling
// into shared root arenas. The native bridge retains normal-return semantics.
func (f *integerEmitter) referenceCall(sig *wasm.CompType, key uint64, base, referenceSlot int, spill func(int) int32, stride int, hiddenRefs []bool) {
	if !f.mode.referenceCalls || !f.mode.foreignTableCalls || !f.mode.invocationContext || !f.mode.production || !f.mode.interruptible || (stride != 8 && stride != 16) {
		f.err = unsupported("call_ref native owner contract")
		return
	}
	f.Load(14, spill(referenceSlot), true)
	// call_ref has its own null-reference trap. Descriptor corruption and owner
	// failures below remain indirect-call bounds/signature failures.
	f.trap(trapNullReference, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
	f.canonicalReferenceGuard(enc.X14, enc.X5, enc.X7)
	f.a.Ld(enc.X28, enc.X14, 0)
	f.a.Addi(enc.X5, enc.Zero, 15)
	f.a.And(enc.X5, enc.X28, enc.X5)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.Zero))
	f.a.Ld(enc.X7, enc.X14, 8)
	f.a.Constant64(enc.X5, int64(key))
	f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	// Complete target home/context/anchor checks before caller arena writes.
	f.a.Ld(enc.X6, enc.X14, 16)
	f.a.Constant64(enc.X5, int64(^abi.FuncRefHomeTagMask))
	f.a.And(enc.X6, enc.X6, enc.X5)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Addi(enc.X5, enc.Zero, 7)
	f.a.And(enc.X5, enc.X6, enc.X5)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.Zero))
	f.a.Ld(enc.X7, enc.X14, 32)
	f.a.Addi(enc.X5, enc.Zero, 7)
	f.a.And(enc.X5, enc.X7, enc.X5)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.Zero))
	if f.mode.gcRootedScalar && referenceCallCarriesCollector(sig) {
		// Raw collector objects are instance-local. Numeric calls may enter any
		// retained wrapper, but reference-bearing calls must prove the callable
		// descriptor names this exact native/collector context before arena writes.
		f.a.Ld(enc.X5, enc.X25, -abi.RV64NativeContextOffset)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	}
	f.a.Ld(enc.X5, enc.X7, 16)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	f.a.Addi(enc.X6, enc.Zero, 7)
	f.a.And(enc.X6, enc.X5, enc.X6)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X6, enc.Zero))
	f.a.Ld(enc.X6, enc.X5, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X6, enc.X7))
	f.a.Ld(enc.X6, enc.X5, 16) // authoritative untagged owner home
	f.a.Ld(enc.X5, enc.X14, 16)
	f.a.Constant64(enc.X7, int64(^abi.FuncRefHomeTagMask))
	f.a.And(enc.X5, enc.X5, enc.X7)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.X6))

	// Argument transport uses X5/X6 and reserved address scratch; X14 survives.
	f.swarCallArguments(sig, base, spill)
	returnOffset, stackAdjust := f.nativeTableCall(key, swarSignatureSlots(sig.Params), swarSignatureSlots(sig.Results))
	if f.mode.gcRootedScalar && returnOffset != 0 {
		f.recordGCFrameCallsiteAdjusted(returnOffset, stackAdjust, hiddenRefs)
	} else {
		f.recordGCFrameCallsite(f.a.Len(), hiddenRefs)
	}
	if stride == 16 {
		f.swarCallResults(sig, base, spill)
		return
	}
	f.restoreCallTransport()
	for j, typ := range sig.Results {
		wide := scalarWide(typ) || f.mode.referenceFunctions && typ.Kind() == wasm.ValRef
		if wide {
			f.a.Ld(enc.X5, enc.X13, int32(j*8))
		} else {
			f.a.Lwu(enc.X5, enc.X13, int32(j*8))
		}
		f.Store(spill(base+j), 5, wide)
	}
}

func referenceCallCarriesCollector(sig *wasm.CompType) bool {
	if sig == nil {
		return false
	}
	for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			if typ.Kind() == wasm.ValRef {
				return true
			}
		}
	}
	return false
}

// functionReferenceCast checks the canonical descriptor's exact structural key
// against the bounded subtype closure admitted for one target indexed heap. The
// carrier itself is unchanged on success.
func (f *integerEmitter) functionReferenceCast(base int, keys []uint64, nullable bool) {
	if !f.mode.referenceCalls || !f.mode.foreignTableCalls || !f.mode.invocationContext || !f.mode.production || !f.mode.interruptible || len(keys) == 0 || len(keys) > 3 {
		f.err = unsupported("function ref.cast owner/signature contract")
		return
	}
	f.Load(14, f.SpillOffset(base), true)
	var success []int
	if nullable {
		success = append(success, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
	} else {
		f.trap(trapI31CastFailure, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
	}
	f.canonicalReferenceGuard(enc.X14, enc.X5, enc.X7)
	f.a.Ld(enc.X7, enc.X14, 8)
	for _, key := range keys {
		f.a.Constant64(enc.X5, int64(key))
		success = append(success, f.a.BranchLong(enc.EQ, enc.X7, enc.X5))
	}
	f.trap(trapI31CastFailure, f.Jump())
	for _, site := range success {
		if err := f.Patch(site, f.Position()); err != nil {
			f.err = err
			return
		}
	}
}

// functionReferenceTest checks one canonical local descriptor against the
// bounded subtype closure and leaves a normalized i32 result. Unlike ref.cast,
// a signature mismatch is false, and null follows the immediate's nullability.
func (f *integerEmitter) functionReferenceTest(base int, keys []uint64, nullable bool) {
	if !f.mode.referenceFunctions || !f.mode.foreignTableCalls || !f.mode.invocationContext || !f.mode.production || !f.mode.interruptible || len(keys) == 0 || len(keys) > 3 {
		f.err = unsupported("function ref.test owner/signature contract")
		return
	}
	f.Load(14, f.SpillOffset(base), true)
	if nullable {
		f.a.Constant32(enc.X6, 1)
	} else {
		f.a.Constant32(enc.X6, 0)
	}
	done := []int{f.a.BranchLong(enc.EQ, enc.X14, enc.Zero)}
	if nullable {
		f.a.Constant32(enc.X6, 0)
	}
	f.canonicalReferenceGuard(enc.X14, enc.X5, enc.X7)
	f.a.Ld(enc.X7, enc.X14, 8)
	var success []int
	for _, key := range keys {
		f.a.Constant64(enc.X5, int64(key))
		success = append(success, f.a.BranchLong(enc.EQ, enc.X7, enc.X5))
	}
	done = append(done, f.Jump())
	for _, site := range success {
		if err := f.Patch(site, f.Position()); err != nil {
			f.err = err
			return
		}
	}
	f.a.Constant32(enc.X6, 1)
	for _, site := range done {
		if err := f.Patch(site, f.Position()); err != nil {
			f.err = err
			return
		}
	}
	f.Store(f.SpillOffset(base), 6, false)
}

// functionReferenceIdentityTest compares a private ref.func carrier with its
// exact canonical arena slot. The admitted source functions are intentionally
// non-callable reference-signature identities, so their rows have no published
// code pointer; the arena base and fixed slot still provide an immutable,
// instance-local identity without manufacturing a callable fallback.
func (f *integerEmitter) functionReferenceIdentityTest(base int, functionIndex uint32, nullable bool) {
	if !f.mode.referenceFunctions || !f.mode.foreignTableCalls || !f.mode.invocationContext || !f.mode.production || !f.mode.interruptible || functionIndex >= 2 {
		f.err = unsupported("function ref.test identity contract")
		return
	}
	f.Load(14, f.SpillOffset(base), true)
	if nullable {
		f.a.Constant32(enc.X6, 1)
	} else {
		f.a.Constant32(enc.X6, 0)
	}
	done := []int{f.a.BranchLong(enc.EQ, enc.X14, enc.Zero)}
	if nullable {
		f.a.Constant32(enc.X6, 0)
	}
	f.a.Ld(enc.X5, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	f.a.Constant64(enc.X7, int64(functionIndex+1)*40)
	f.a.Add(enc.X5, enc.X5, enc.X7)
	done = append(done, f.a.BranchLong(enc.NE, enc.X14, enc.X5))
	f.a.Constant32(enc.X6, 1)
	for _, site := range done {
		if err := f.Patch(site, f.Position()); err != nil {
			f.err = err
			return
		}
	}
	f.Store(f.SpillOffset(base), 6, false)
}
