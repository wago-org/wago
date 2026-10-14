//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// swarCompactGCArguments copies the low scalar/reference cell from each
// padded SWAR operand home into the contiguous scalar slots consumed by the
// shared GC helper emitters. Destinations never exceed their sources, so the
// forward copy is overlap-safe. Operands are consumed by the helper; only its
// result remains in the first logical home.
func (f *integerEmitter) swarCompactGCArguments(base, count int, spill func(int) int32) {
	for i := 0; i < count; i++ {
		f.Load(5, spill(base+i), true)
		f.Store(f.SpillOffset(base*2+i), 5, true)
	}
}

func swarCallSignature(sig *wasm.CompType) error {
	if sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > maxSWARLocals || len(sig.Results) > maxSWARLocals {
		return unsupported("SWAR direct call signature shape/bound")
	}
	for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range list {
			if !swarCarrierType(typ) {
				return unsupported("SWAR direct call signature carrier")
			}
		}
	}
	return nil
}

// Caller values remain in padded stack homes while compact shared arenas are
// overwritten by the callee. Narrow carriers always fill a zero-normalized
// complete cell, and vector halves remain raw. Only same-image framed entries
// are relocation targets; the production trap bridge unwinds nested frames.
func (f *integerEmitter) swarDirectCall(index uint32, sig *wasm.CompType, base int, spill func(int) int32, hiddenRefs []bool) {
	f.swarCallArguments(sig, base, spill)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), callee: index})
	f.recordGCFrameCallsite(f.a.Len(), hiddenRefs)
	f.swarCallResults(sig, base, spill)
}

func (f *integerEmitter) swarCallArguments(sig *wasm.CompType, base int, spill func(int) int32) {
	f.restoreCallTransport()
	offset := int32(0)
	for i, typ := range sig.Params {
		class := swarClassOf(typ)
		if (f.mode.referenceFunctions || f.mode.gcRootedScalar) && typ.Kind() == wasm.ValRef {
			class = swarFuncRef
		}
		f.Load(5, spill(base+i), !class.narrow())
		f.a.Sd(enc.X5, enc.X10, offset)
		offset += 8
		if class == swarV128 {
			f.Load(6, spill(base+i)+8, true)
			f.a.Sd(enc.X6, enc.X10, offset)
			offset += 8
		}
	}
}

func (f *integerEmitter) swarCallResults(sig *wasm.CompType, base int, spill func(int) int32) {
	f.restoreCallTransport()
	offset := int32(0)
	for i, typ := range sig.Results {
		class := swarClassOf(typ)
		if (f.mode.referenceFunctions || f.mode.gcRootedScalar) && typ.Kind() == wasm.ValRef {
			class = swarFuncRef
		}
		if class.narrow() {
			f.a.Lwu(enc.X5, enc.X13, offset)
		} else {
			f.a.Ld(enc.X5, enc.X13, offset)
		}
		f.Store(spill(base+i), 5, true)
		offset += 8
		if class == swarV128 {
			f.a.Ld(enc.X6, enc.X13, offset)
			offset += 8
		} else {
			f.a.Addi(enc.X6, enc.Zero, 0)
		}
		f.Store(spill(base+i)+8, 6, true)
	}
}

func (f *integerEmitter) swarIndirectCall(sig *wasm.CompType, key uint64, base, indexSlot int, spill func(int) int32, hiddenRefs []bool) {
	f.Load(5, spill(indexSlot), false)
	f.ownedIndirectTarget(key)
	f.swarCallArguments(sig, base, spill)
	returnOffset, stackAdjust := 0, uint32(0)
	if f.mode.foreignTableCalls {
		params, results := len(sig.Params), len(sig.Results)
		for _, typ := range sig.Params {
			if wasm.EqualValType(typ, wasm.V128) {
				params++
			}
		}
		for _, typ := range sig.Results {
			if wasm.EqualValType(typ, wasm.V128) {
				results++
			}
		}
		returnOffset, stackAdjust = f.nativeTableCall(key, params, results)
	} else {
		f.a.Jalr(enc.RA, enc.X28, 0)
	}
	if f.mode.gcRootedScalar && returnOffset != 0 {
		f.recordGCFrameCallsiteAdjusted(returnOffset, stackAdjust, hiddenRefs)
	} else {
		f.recordGCFrameCallsite(f.a.Len(), hiddenRefs)
	}
	f.swarCallResults(sig, base, spill)
}

// The qualified scalar import guards establish code, home and exact current
// context identity before compact argument writes. X28 survives escaped stack
// homes; raw vector values stay in caller homes across the parked host bridge.
func (f *integerEmitter) swarImportCall(index uint32, sig *wasm.CompType, base int, spill func(int) int32, hiddenRefs []bool) {
	f.importCallTarget(index)
	f.swarCallArguments(sig, base, spill)
	f.a.Jalr(enc.RA, enc.X28, 0)
	f.recordGCFrameCallsite(f.a.Len(), hiddenRefs)
	f.swarCallResults(sig, base, spill)
}
