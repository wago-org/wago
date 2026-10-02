//go:build arm64 && wago_regalloccheck

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
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
	f.ld64(X8, SP, f.spillOff(4))
	f.regUser[X8] = a
	requireAllocationFailure(t, "materialize transfer", func() { f.occupy(a, X8) })
	f.a.ObserveRegalloc(nil)
}

func TestRegallocCheckRejectsCanonicalOverwrite(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	a := f.pushReg(X8, mtI64)
	b := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	f.checkBeginFlush([]*elem{a, b})
	// The old canonicalization family wrote an earlier root over a later root's
	// spill. Neither root's allocator metadata changes at this machine store.
	f.st64(SP, f.spillOff(0), X8)
	requireAllocationFailure(t, "materialize input", func() { f.materialize(b) })
	f.a.ObserveRegalloc(nil)
}

func TestRegallocCheckRejectsCallClobberedPreload(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	f.preloadFloatConst(storage{kind: stConst, typ: mtF64, cval: 0x3ff0000000000000})
	// Fault injection reproduces incorrectly admitting an immutable cache in a
	// function that later emits a physical call. The identity was made at preload.
	requireAllocationFailure(t, "immutable cache across physical call", func() { f.emitRegisterCallVia(&wasm.CompType{}, -1, false, 0, regNone) })
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
	a := f.pushReg(X8, mtI64)
	b := f.pushReg(X9, mtI64)
	f.checkBeginFlush([]*elem{a, b})
	f.st64(SP, f.spillOff(10), X8)
	f.st64(SP, f.spillOff(11), X8) // controlled fault: second staging slot receives a, not b
	f.moveSlots(10, 0, 2)
	// The edge-copy seam must retain the enclosing window's original identities.
	requireAllocationFailure(t, "canonical stack", func() { f.checkEndFlush() })
}

func TestRegallocCheckProtectsSuffixAtWindowExit(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	prefix := f.pushReg(X8, mtI64)
	f.pushReg(X0, mtI64)
	f.checkBeginFlush([]*elem{prefix})
	f.st64(SP, f.spillOff(0), X8)
	f.ld64(X0, SP, f.spillOff(0)) // fault: scratch reuses the still-live condition register without restoring it
	requireAllocationFailure(t, "live suffix", func() { f.checkEndFlush() })
}
