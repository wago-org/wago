//go:build amd64 && wago_regalloccheck

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
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

func TestRegallocCheckLoopCacheRetirement(t *testing.T) {
	t.Run("retired-cache", func(t *testing.T) {
		f := fn{}
		f.checkImmutable(0, true, 8)
		f.checkReleaseImmutable(0, true)
		f.checkCallClobber()
		// A later preload into the same physical register starts a new lifetime.
		f.checkImmutable(0, true, 8)
		requireAllocationFailure(t, "immutable cache across physical call", f.checkCallClobber)
	})
	t.Run("outer-cache-remains-live", func(t *testing.T) {
		f := fn{}
		f.checkImmutable(0, true, 8)
		f.checkImmutable(1, true, 8)
		outer := f.immutableValues[0]
		f.checkReleaseImmutable(1, true)
		if len(f.immutableValues) != 1 || &f.immutableValues[0].value[0] != &outer.value[0] {
			t.Fatal("retiring an inner cache changed the outer cache identity")
		}
		requireAllocationFailure(t, "immutable cache across physical call", f.checkCallClobber)
	})
	t.Run("separate-register-banks", func(t *testing.T) {
		f := fn{}
		f.checkImmutable(0, false, 8)
		f.checkImmutable(0, true, 8)
		f.checkReleaseImmutable(0, true)
		if len(f.immutableValues) != 1 || f.immutableValues[0].loc != checkReg(0, false) {
			t.Fatal("retiring an FP cache lost a GP cache")
		}
		requireAllocationFailure(t, "immutable cache across physical call", f.checkCallClobber)
	})
}

// An outer emission journal must see each effect exactly once inside a nested
// window, and be restored when the window closes or unwinds.
func TestRegallocWindowsForwardPhysicalEffects(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	var observed []regalloccheck.Effect
	f.a.ObserveRegalloc(func(e regalloccheck.Effect) { observed = append(observed, e) })
	closeSlots := f.checkBeginSlots(0, 0, 1)
	f.a.Load64(R8, RSP, f.spillOff(0))
	if len(observed) != 1 || observed[0].Kind != regalloccheck.Copy {
		t.Fatalf("slot observer swallowed effect: %+v", observed)
	}
	closeSlots()
	observed = nil
	func() {
		f.checkBeginFlush(nil)
		defer f.checkEndFlush()
		f.a.Load64(R8, RSP, f.spillOff(0))
		f.a.AluRM(0x3b, R8, RSP, f.spillOff(0), true)
		if len(observed) != 2 || observed[1].Kind != regalloccheck.Read {
			t.Fatalf("folded read not forwarded: %+v", observed)
		}
		f.a.CallReg(R9)
	}()
	if observed[len(observed)-1].Kind != regalloccheck.Call {
		t.Fatalf("flush observer swallowed call: %+v", observed)
	}
	n := len(observed)
	f.a.CallReg(R9)
	if len(observed) != n+1 {
		t.Fatal("outer observer not restored")
	}
	observed = nil
	func() {
		defer func() {
			if recover() != "controlled unwind" {
				t.Fatal("lost original panic")
			}
		}()
		f.checkBeginFlush(nil)
		defer f.checkEndFlush()
		panic("controlled unwind")
	}()
	f.a.CallReg(R9)
	if len(observed) != 1 || observed[0].Kind != regalloccheck.Call {
		t.Fatalf("outer observer not restored on panic: %+v", observed)
	}
}
