//go:build arm64

package arm64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// These are emission tests for an explicit X11 pin. They inspect generated
// instructions and stack metadata; they never execute an incomplete function.
func syncHostPinEmitter() *fn {
	return &fn{
		a: &a64.Asm{}, s: newStackWithCap(minStackArenaCap), sc: &scratch{},
		m: &wasm.Module{}, ft: &wasm.CompType{Kind: wasm.CompFunc},
		memSizeReg: regNone, globalCellReg: regNone, syncHostSlots: maxSyncHostSlots,
	}
}

func TestSyncHostRestoresX11LocalAfterResults(t *testing.T) {
	for _, lazy := range []bool{false, true} {
		t.Run(fmt.Sprintf("stack-reg=%t", lazy), func(t *testing.T) {
			f := syncHostPinEmitter()
			f.usesCalls = lazy
			f.nLocals, f.nLocalSlots = 1, 1
			f.localSlot, f.localType = []uint32{0}, []machineType{mtI64}
			f.locals = []localDef{{reg: X11, state: lsReg}}
			f.pinnedLocalMask = maskOf(X11)
			if reg, fp, ok := f.pinReg(0); !ok || fp || reg != X11 {
				t.Fatal("fixture has no X11 local pin")
			}
			if err := f.callHostSync(0, &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}); err != nil {
				t.Fatal(err)
			}
			var reload a64.Asm
			reload.Load64(X11, SP, uint32(f.localOff(0)))
			if lazy {
				if f.locals[0].state != lsMem || bytes.Contains(f.a.B, reload.B) {
					t.Fatal("lazy local must remain homed until its next read")
				}
			} else if !bytes.HasSuffix(f.a.B, reload.B) {
				t.Fatal("eager X11 local was not restored after result loads")
			}
			if f.depth() != 1 || f.s.back().st.reg == X11 || f.pinned.has(X11) {
				t.Fatal("result or temporary control-base pin overlaps the X11 local")
			}
		})
	}
}

func TestSyncHostRestoresX11GlobalAfterResults(t *testing.T) {
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, lazy := range []bool{false, true} {
		t.Run(fmt.Sprintf("stack-reg=%t", lazy), func(t *testing.T) {
			f := syncHostPinEmitter()
			f.m, f.usesCalls = m, lazy
			f.globalReg = []Reg{X11 | globalRegDirty}
			f.pinnedLocalMask = maskOf(X11)
			if globalRegValue(f.globalReg[0]) != X11 || f.isModuleGlobal(0) {
				t.Fatal("fixture has no function-local X11 global pin")
			}
			if err := f.callHostSync(0, &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}); err != nil {
				t.Fatal(err)
			}
			want := &fn{a: &a64.Asm{}}
			want.ld64(X11, linMemReg, -int32(abi.GlobalsPtrOffset))
			want.ld64(X11, X11, 0)
			want.ld64(X11, X11, 0)
			if !bytes.HasSuffix(f.a.B, want.a.B) {
				t.Fatal("X11 global was not restored after result loads")
			}
			if f.depth() != 1 || f.s.back().st.reg == X11 || f.pinned.has(X11) {
				t.Fatal("result or temporary control-base pin overlaps the X11 global")
			}
		})
	}
}

func TestSyncHostResultSpillsPreserveRootMetadata(t *testing.T) {
	f := syncHostPinEmitter()
	plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true}
	if !plan.SetLiveMasks([]uint64{0, 0}, 0, 2) {
		t.Fatal("invalid test root plan")
	}
	f.gcFrameRoots = plan
	prefix := f.pushValue(storage{kind: stConst, typ: mtI64})
	prefix.st.setGCRoot(true)
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: 17})
	results := make([]wasm.ValType, 48)
	for i := range results {
		results[i] = []wasm.ValType{wasm.AnyRef, wasm.I64, wasm.ExternRef}[i%3]
	}
	if err := f.callHostSync(0, &wasm.CompType{Kind: wasm.CompFunc, Results: results}); err != nil {
		t.Fatal(err)
	}
	roots := f.rootsBottomToTop()
	if len(roots) != 50 || !roots[0].st.hasGCRoot() || roots[1].st.hasGCRoot() {
		t.Fatal("host call changed live prefix roots")
	}
	spilledRoots := 0
	for i, value := range roots[2:] {
		want := i%3 == 0
		if value.st.hasGCRoot() != want {
			t.Fatalf("result %d root=%t want %t", i, value.st.hasGCRoot(), want)
		}
		if want && value.st.kind == stSlot {
			spilledRoots++
		}
	}
	if spilledRoots == 0 {
		t.Fatal("fixture did not spill any reference result")
	}
	// A later call must still publish the live prefix plus the 16 anyref
	// results. Numeric values and externref handles are not collector roots.
	offsets, ok := f.prepareGCFrameCallsite(0)
	if !ok || !plan.Exact || len(offsets) != 17 {
		t.Fatalf("later call root offsets=%v exact=%t admitted=%t", offsets, plan.Exact, ok)
	}
	if offsets[0] != uint32(f.spillOff(0)) {
		t.Fatal("later call lost the live prefix root")
	}
	for i := 0; i < 16; i++ {
		if offsets[i+1] != uint32(f.spillOff(2+3*i)) {
			t.Fatalf("result root %d has offset %d", i, offsets[i+1])
		}
	}
}
