//go:build amd64 && wago_regalloccheck

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"strings"
	"testing"
)

func requireAllocationFailure(t *testing.T, part string, action func()) {
	t.Helper()
	defer func() {
		failure := recover()
		if failure == nil || !strings.Contains(fmt.Sprint(failure), "regalloccheck:") || !strings.Contains(fmt.Sprint(failure), part) {
			t.Fatalf("expected %q checker failure, got %v", part, failure)
		}
	}()
	action()
}

func TestRegallocCheckRejectsWrongReloadDespiteMatchingOwner(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	a := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
	b := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
	f.checkBeginFlush([]*elem{a, b})
	// Model the bug at the machine-effect seam: metadata names a, but the load
	// reads b's slot. Matching allocator ownership must not bless that value.
	f.a.Load64(R8, RSP, f.spillOff(4))
	f.regUser[R8] = a
	requireAllocationFailure(t, "materialize transfer", func() { f.occupy(a, R8) })
	f.a.ObserveRegalloc(nil)
}

func TestRegallocCheckRejectsCanonicalOverwrite(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	a := f.pushReg(R8, mtI64)
	b := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	f.checkBeginFlush([]*elem{a, b})
	// The old canonicalization family wrote an earlier root over a later root's
	// spill. Neither root's allocator metadata changes at this machine store.
	f.a.Store64(RSP, f.spillOff(0), R8)
	requireAllocationFailure(t, "materialize input", func() { f.materialize(b) })
	f.a.ObserveRegalloc(nil)
}

func TestRegallocCheckRejectsCallClobberedPreload(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	f.preloadFloatConst(storage{kind: stConst, typ: mtF64, cval: 0x3ff0000000000000})
	// Fault injection reproduces incorrectly admitting an immutable cache in a
	// function that later emits a physical call. The identity was made at preload.
	requireAllocationFailure(t, "immutable cache across physical call", func() { f.emitRegisterCallVia(&wasm.CompType{}, -1, 0, regNone) })
}

func TestRegallocCheckAcceptsFlushAndEdgeMoves(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	for i := 0; i < 12; i++ {
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(i + 1)})
	}
	f.flush()
	f.moveSlots(4, 0, 8)
}

func TestRegallocCheckRejectsOverlappingForwardEdgeCopy(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	// Existing callers copy downwards. A future caller requesting an overlapping
	// upwards parallel assignment must not silently use the forward-only loop.
	requireAllocationFailure(t, "control-edge slot copy", func() { f.moveSlots(0, 1, 3) })
}

func TestRegallocCheckStagedCopyDoesNotReseed(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	a := f.pushReg(R8, mtI64)
	b := f.pushReg(R9, mtI64)
	f.checkBeginFlush([]*elem{a, b})
	f.a.Store64(RSP, f.spillOff(10), R8)
	f.a.Store64(RSP, f.spillOff(11), R8) // controlled fault: second staging slot receives a, not b
	f.moveSlots(10, 0, 2)
	// The edge-copy seam must retain the enclosing window's original identities.
	requireAllocationFailure(t, "canonical stack", func() { f.checkEndFlush() })
}

func TestRegallocCheckProtectsSuffixAtWindowExit(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	prefix := f.pushReg(R8, mtI64)
	f.pushReg(RAX, mtI64)
	f.checkBeginFlush([]*elem{prefix})
	f.a.Store64(RSP, f.spillOff(0), R8)
	f.a.Load64(RAX, RSP, f.spillOff(0)) // fault: scratch reuses the still-live condition register without restoring it
	requireAllocationFailure(t, "live suffix", func() { f.checkEndFlush() })
}
