//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Only compiler state and byte emission are used; native code is never run.
func tableRootEmitter(typ wasm.ValType, imported bool) *fn {
	table := wasm.TableType{Ref: typ.Ref()}
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{
		{Comp: wasm.CompType{Kind: wasm.CompStruct}},
		{Comp: wasm.CompType{Kind: wasm.CompArray, Array: wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var)}},
		{Comp: wasm.CompType{Kind: wasm.CompFunc}},
	}}}}
	if imported {
		m.Imports = []wasm.Import{{Type: wasm.NewTableExternType(table)}}
	} else {
		m.Tables = []wasm.Table{{Type: table}}
	}
	f := &fn{a: &x86.Asm{}, s: newStackWithCap(minStackArenaCap), sc: &scratch{}, m: m, memSizeReg: regNone, globalCellReg: regNone}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 17})
	f.pushValue(storage{kind: stConst, typ: mtI32})
	return f
}

func TestTableGetResultRoots(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  wasm.ValType
		root bool
	}{
		{"any", wasm.AnyRef, true}, {"eq", wasm.EqRef, true}, {"i31", wasm.I31Ref, true},
		{"struct", wasm.RefVal(wasm.AbsRef(wasm.HeapStruct)), true},
		{"array", wasm.RefVal(wasm.AbsRef(wasm.HeapArray)), true},
		{"none", wasm.RefVal(wasm.AbsRef(wasm.HeapNone)), true},
		{"defined-struct", wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)), true},
		{"defined-array", wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false)), true},
		{"defined-func", wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false)), false},
		{"func", wasm.FuncRef, false}, {"extern", wasm.ExternRef, false},
	} {
		for _, imported := range []bool{false, true} {
			name := tc.name + "/local"
			if imported {
				name = tc.name + "/imported"
			}
			t.Run(name, func(t *testing.T) {
				f := tableRootEmitter(tc.typ, imported)
				if err := f.tableGet(wasm.NewReader([]byte{0})); err != nil {
					t.Fatal(err)
				}
				if f.s.back().st.hasGCRoot() != tc.root {
					t.Fatalf("table result root = %v, want %v", f.s.back().st.hasGCRoot(), tc.root)
				}
				plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true}
				if !plan.SetLiveMasks([]uint64{0, 0}, 1, 1) {
					t.Fatal("invalid test root plan")
				}
				f.gcFrameRoots = plan
				f.pushValue(storage{kind: stConst, typ: mtI32})
				f.recordGCFrameSafepoint(1)
				seen := false
				plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
					seen = len(offsets) == 0 && !tc.root || len(offsets) == 1 && tc.root && offsets[0] == uint32(f.spillOff(1))
					return true
				})
				if !plan.Exact || !seen {
					t.Fatal("later root map does not match the table result type")
				}
				offsets, ok := f.prepareGCFrameCallsite(1)
				if !ok || tc.root && (len(offsets) != 1 || offsets[0] != uint32(f.spillOff(1))) || !tc.root && len(offsets) != 0 {
					t.Fatal("later callsite root map does not match the table result type")
				}
			})
		}
	}
}

func BenchmarkTableGetRootEmission(b *testing.B) {
	for _, tc := range []struct {
		name string
		typ  wasm.ValType
	}{{"collector", wasm.AnyRef}, {"function", wasm.FuncRef}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				f := tableRootEmitter(tc.typ, false)
				if err := f.tableGet(wasm.NewReader([]byte{0})); err != nil {
					b.Fatal(err)
				}
				if f.a.Len() == 0 {
					b.Fatal("no emission")
				}
			}
		})
	}
}
