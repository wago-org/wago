//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
	"unsafe"
)

// The transfer plan may retain allRoots' suffix only if materialization keeps
// logical root identities and does not borrow the tmpRoots backing store.
func TestCallArgumentRootSuffixSurvivesMaterialization(t *testing.T) {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), m: &wasm.Module{}, localSlot: []uint32{0}, nLocalSlots: 1, globalCellReg: regNone, memSizeReg: regNone, policy: currentCodegenPolicy()}
	f.tmpRoots = make([]*elem, 0, 16)
	prefix := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 0})
	first := f.pushReg(RCX, mtI32)
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 8})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	f.pushBinOp(opShl, mtI32)
	second := f.s.back()
	all := f.rootsBottomToTop()
	if len(all) != 3 || all[0] != prefix || all[1] != first || all[2] != second {
		t.Fatal("fixture roots")
	}
	backing := &f.tmpRoots[0]
	args := all[1:]
	types := f.logicalTypes(all)
	if len(types) != 3 || types[0] != mtV128 || types[0].stackSlots() != 2 {
		t.Fatal("wide prefix fixture")
	}
	f.storePinnedGlobals(false)
	f.materializeCallExpressions(all)
	if len(f.tmpRoots) != 3 || &f.tmpRoots[0] != backing || all[0] != prefix || args[0] != first || args[1] != second {
		t.Fatal("scratch root suffix was overwritten or rebased")
	}
	if first.st.kind != stSlot {
		t.Fatal("later variable shift did not exercise fixed-register spill of first argument")
	}
	if second.isDeferred() {
		t.Fatal("argument expression not materialized")
	}
	if f.s.logicalDepth != 3 {
		t.Fatal("logical roots changed")
	}
	// Independently rebuild physical-root order only after checking retained data.
	got := f.rootsBottomToTop()
	if len(got) != 3 || got[0] != prefix || got[1] != first || got[2] != second {
		t.Fatal("physical root identity/order changed")
	}
}

func TestRegisterArgumentPlanOwnsDeferredHandles(t *testing.T) {
	makeFixture := func() (*fn, *registerArgumentPlan) {
		f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), m: &wasm.Module{}, localSlot: []uint32{0}, localType: []machineType{mtI64}, nLocalSlots: 1, globalCellReg: regNone, memSizeReg: regNone, policy: currentCodegenPolicy()}
		f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 4})
		slot := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
		literal := f.pushValue(storage{kind: stConst, typ: mtF64, cval: 0x7ff8123456789abc})
		local := f.pushValue(storage{kind: stLocalRef, typ: mtI64, idx: 0})
		roots := f.rootsBottomToTop()
		backing := &f.tmpRoots[0]
		plan := &registerArgumentPlan{gpMoves: make([]regMove, 0, 8), fpMoves: make([]regMove, 0, 8), deferred: make([]deferredArg, 0, 16)}
		captured, firstArg := f.captureRegisterArguments(*plan, roots, []wasm.ValType{wasm.I64, wasm.F64, wasm.I64}, registerArgumentsMixed)
		*plan = captured
		if firstArg != slot {
			t.Fatal("capture returned wrong argument suffix")
		}
		if len(f.tmpRoots) != 4 || &f.tmpRoots[0] != backing {
			t.Fatal("capture rebased root scratch")
		}
		if slot.st.kind != stReg || len(plan.gpMoves) != 1 || len(plan.fpMoves) != 0 || len(plan.deferred) != 2 || plan.gpArgs != 2 {
			t.Fatal("slot capture or plan bank counts incorrect")
		}
		if plan.deferred[0].root != literal || !plan.deferred[0].float || plan.deferred[1].root != local || plan.deferred[1].float {
			t.Fatal("deferred handles/order differ")
		}
		return f, plan
	}
	a, ap := makeFixture()
	b, bp := makeFixture()
	// A later helper may reuse root scratch; transfer records must own copies of
	// the handles, not a root-slice/index view into that scratch.
	clear(b.tmpRoots)
	a.commitRegisterArguments(ap)
	b.commitRegisterArguments(bp)
	if !bytes.Equal(a.a.B, b.a.B) || a.pinned != b.pinned || a.fpinned != b.fpinned {
		t.Fatal("commit relied on overwritten root scratch")
	}
}

func TestDeferredArgumentRecordRemainsCompact(t *testing.T) {
	if unsafe.Sizeof(deferredArg{}) != 16 {
		t.Fatalf("deferred argument grew to %d bytes", unsafe.Sizeof(deferredArg{}))
	}
}
