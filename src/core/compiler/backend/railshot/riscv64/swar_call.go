//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

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
func (f *integerEmitter) swarDirectCall(index uint32, sig *wasm.CompType, base int, spill func(int) int32) {
	f.restoreCallTransport()
	offset := int32(0)
	for i, typ := range sig.Params {
		class := swarClassOf(typ)
		f.Load(5, spill(base+i), !class.narrow())
		f.a.Sd(enc.X5, enc.X10, offset)
		offset += 8
		if class == swarV128 {
			f.Load(6, spill(base+i)+8, true)
			f.a.Sd(enc.X6, enc.X10, offset)
			offset += 8
		}
	}
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), callee: index})
	f.restoreCallTransport()
	offset = 0
	for i, typ := range sig.Results {
		class := swarClassOf(typ)
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
