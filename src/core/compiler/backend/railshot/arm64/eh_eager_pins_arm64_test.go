//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestEHCatchRouteRestoresEagerPinsARM64(t *testing.T) {
	for _, makesCalls := range []bool{false, true} {
		f := fn{
			a: &a64.Asm{}, s: newStack(), makesCalls: makesCalls, moduleEH: true,
			nLocals: 2, nLocalSlots: 2, localSlot: []uint32{0, 1},
			localType:       []machineType{mtI64, mtF64},
			locals:          []localDef{{reg: X19}, {reg: 8, isFloat: true}},
			pinnedLocalMask: maskOf(X19), fpinnedLocalMask: maskOf(8),
			ctrl: []ctrlFrame{{kind: cfBlock}},
		}
		f.emitEHCatchRoute(&ctrlFrame{}, &ehCatchClause{kind: wasm.CatchAll}, f.ehRecordOff(0))
		var loads a64.Asm
		loads.Load64(X19, SP, uint32(f.localOff(0)))
		loads.LdrD(8, SP, f.localOff(1))
		if got := bytes.Contains(f.a.B, loads.B); got != makesCalls {
			t.Fatalf("makesCalls=%v: catch route restores GP/FP pins=%v, code=%x", makesCalls, got, f.a.B)
		}
	}
}

func TestEHThrowPublishesEagerPinsARM64(t *testing.T) {
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, throwRef := range []bool{false, true} {
		for _, makesCalls := range []bool{false, true} {
			f := fn{
				a: &a64.Asm{}, s: newStack(), m: m,
				makesCalls: makesCalls, moduleEH: true,
				nLocals: 2, nLocalSlots: 2, localSlot: []uint32{0, 1},
				localType:       []machineType{mtI64, mtF64},
				locals:          []localDef{{reg: X19}, {reg: 8, isFloat: true}},
				pinnedLocalMask: maskOf(X19), fpinnedLocalMask: maskOf(8),
			}
			if throwRef {
				f.pushValue(storage{kind: stConst, typ: mtI64, cval: 1})
				err = f.opThrowRef()
			} else {
				err = f.opThrow(wasm.NewReader([]byte{0}))
			}
			if err != nil {
				t.Fatal(err)
			}
			var stores a64.Asm
			stores.Store64(X19, SP, uint32(f.localOff(0)))
			stores.StrD(SP, f.localOff(1), 8)
			if got := bytes.HasPrefix(f.a.B, stores.B); got != makesCalls {
				t.Fatalf("throw-ref=%v makesCalls=%v: publishes GP/FP pins=%v, code=%x", throwRef, makesCalls, got, f.a.B)
			}
		}
	}
}

func TestEHTryDisablesPinPreservingCallARM64(t *testing.T) {
	f := fn{calleeHints: []funcHints{{flags: hintPreservesCallerPins}}}
	if !f.directCalleePreservesPins(0) {
		t.Fatal("ordinary leaf call lost its pin-preserving path")
	}
	f.ehTryDepth = 1
	if f.directCalleePreservesPins(0) {
		t.Fatal("call inside try_table can throw past its pin-preserving return")
	}
}
