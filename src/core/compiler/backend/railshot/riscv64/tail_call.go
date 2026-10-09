//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// checkedFrameBytes is shared by framing and proper local-tail release. Bound
// each term before arithmetic, including callers supplied by private probes.
func checkedFrameBytes(header, locals, slots int) (int, error) {
	if header == 0 {
		header = 16
	}
	if header < 16 || header%8 != 0 || header > MaxFrameBytes || locals < 0 || slots < 0 {
		return 0, fmt.Errorf("rv64: invalid frame layout")
	}
	remaining := MaxFrameBytes - header
	if locals > remaining/8 {
		return 0, fmt.Errorf("frame exceeds %d bytes", MaxFrameBytes)
	}
	remaining -= 8 * locals
	if slots > remaining/8 {
		return 0, fmt.Errorf("frame exceeds %d bytes", MaxFrameBytes)
	}
	frame := header + 8*locals + 8*slots
	frame = (frame + 15) &^ 15
	if frame > MaxFrameBytes {
		return 0, fmt.Errorf("frame exceeds %d bytes", MaxFrameBytes)
	}
	return frame, nil
}

// C2a accepts only exact four-scalar defined-target signatures. The internal
// switch does not advertise the public tail-call family or vector/import tails.
func admitLocalTailSignature(caller, callee *wasm.CompType) error {
	if caller == nil || callee == nil || caller.Kind != wasm.CompFunc || callee.Kind != wasm.CompFunc || len(caller.Results) != len(callee.Results) || len(callee.Params) > 256 || len(callee.Results) > 256 {
		return unsupported("local tail signature shape")
	}
	for _, list := range [][]wasm.ValType{callee.Params, callee.Results} {
		for _, typ := range list {
			if !scalarType(typ, true) {
				return unsupported("local tail numeric signature required")
			}
		}
	}
	for i, typ := range caller.Results {
		if !wasm.EqualValType(typ, callee.Results[i]) {
			return unsupported("local tail exact results required")
		}
	}
	return nil
}

// The SWAR path preserves exact scalar/vector classes and compact pair slots.
// Both signatures are bounded before any array use or physical-cell sum; the
// scalar Boolean-width walker retains the separate numeric-only helper above.
func admitSWARLocalTailSignature(caller, callee *wasm.CompType) error {
	if err := swarCallSignature(caller); err != nil {
		return err
	}
	if err := swarCallSignature(callee); err != nil {
		return err
	}
	if len(caller.Results) != len(callee.Results) {
		return unsupported("SWAR local tail exact results required")
	}
	for i, typ := range caller.Results {
		if !wasm.EqualValType(typ, callee.Results[i]) {
			return unsupported("SWAR local tail exact results required")
		}
	}
	return nil
}

// Only the private same-image function mode uses this check, after both
// signatures have passed its bounded directory admission. Preserve exact heap
// and nullability identity even though all function refs share one carrier.
func admitReferenceTailResults(caller, callee *wasm.CompType) error {
	if caller == nil || callee == nil || caller.Kind != wasm.CompFunc || callee.Kind != wasm.CompFunc || len(caller.Results) > 256 || len(caller.Results) != len(callee.Results) {
		return unsupported("private reference tail result shape/bound")
	}
	for i, typ := range caller.Results {
		if !wasm.EqualValType(typ, callee.Results[i]) {
			return unsupported("private reference tail exact results required")
		}
	}
	return nil
}

func (f *integerEmitter) admitLocalTailFrame() bool {
	if f.err != nil || f.a.Err() != nil {
		return false
	}
	if !f.mode.localTailCalls || !f.mode.production || !f.mode.interruptible || f.frameHeader() != callFrameHeader || f.frameBytes < callFrameHeader || f.frameBytes > MaxFrameBytes || f.frameBytes%16 != 0 {
		f.err = unsupported("local tail requires a finalized cooperative production frame")
		return false
	}
	return true
}

func (f *integerEmitter) localTailCall(index uint32, sig functionSpec, base, importedFunctions int) {
	if !f.admitLocalTailFrame() {
		return
	}
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	f.localTailJump(index - uint32(importedFunctions))
}

func (f *integerEmitter) swarLocalTailCall(index uint32, sig *wasm.CompType, base int, spill func(int) int32) {
	if !f.admitLocalTailFrame() {
		return
	}
	f.swarCallArguments(sig, base, spill)
	f.localTailJump(index)
}

// Arguments have been copied while every source home was live. Forward the
// original result destination and the saved incoming RA, release exactly this
// frame, then enter the target's checked wrapper without creating a new link.
// No discarded home is read after SP changes; no result-copy continuation exists.
func (f *integerEmitter) localTailJump(index uint32) {
	if !f.admitLocalTailFrame() {
		return
	}
	f.restoreCallTransport()
	f.a.Ld(enc.RA, enc.SP, 0)
	f.a.Constant64(enc.Scratch0, int64(f.frameBytes))
	f.a.Add(enc.SP, enc.SP, enc.Scratch0)
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(false), callee: index})
}

// Private owned-table tails select a same-instance framed wrapper. Dynamic
// bounds/type/context guards finish before compact arenas are overwritten.
// The target survives marshalling in X28; no reserved Go register is used.
func (f *integerEmitter) localIndirectTailCall(sig *wasm.CompType, key uint64, base, indexSlot int, spill func(int) int32) {
	if !f.admitLocalTailFrame() {
		return
	}
	if f.mode.foreignTableCalls || spill == nil || sig == nil {
		f.err = unsupported("owned indirect tail requires local framed target")
		return
	}
	f.Load(5, spill(indexSlot), false)
	f.ownedIndirectTarget(key)
	f.a.Constant64(enc.X5, 15)
	f.a.And(enc.X5, enc.X28, enc.X5)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.Zero))
	f.swarCallArguments(sig, base, spill)
	f.restoreCallTransport()
	f.a.Ld(enc.RA, enc.SP, 0)
	f.a.Constant64(enc.Scratch0, int64(f.frameBytes))
	f.a.Add(enc.SP, enc.SP, enc.Scratch0)
	f.a.Jalr(enc.Zero, enc.X28, 0)
}
